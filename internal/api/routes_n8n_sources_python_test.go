package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestPythonNativeSourceWorkflowsRecoverPoliciesAndUseLinkedAccounts(t *testing.T) {
	python, packagePath := nativeProducerRuntime(t)
	config.InitializeEmpty()
	directory := t.TempDir()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(directory, "source-workflows.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	service := ingest.New(repo)
	var root *models.MediaRoot
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Fixture root", State: "active"}})
		require.NoError(t, err)
		producer, err = repo.Ingest.CreateProducer(ctx, "Fixture n8n sources")
		return err
	}))
	_, token, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	application := http.StripPrefix("/api/v3/archive", (&nativeArchiveRoutes{repo: repo}).router())
	producerRoutes := (&ingestRoutes{service: service}).router()
	var dropped atomic.Bool
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, ingestPath+"/") {
			if r.Header.Get("ApiKey") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			producerRoutes.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("ApiKey") != "fixture-application-key" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		recorder := httptest.NewRecorder()
		application.ServeHTTP(recorder, r)
		if r.Method == http.MethodPut && recorder.Code == http.StatusOK {
			writes.Add(1)
			if strings.HasSuffix(r.URL.Path, "/metadata-policy") && !dropped.Swap(true) {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
				return
			}
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	t.Cleanup(server.Close)
	run := func(phase string) {
		t.Helper()
		setup, err := json.Marshal(map[string]any{"directory": directory, "root": root.UUID, "producer": producer.UUID, "endpoint": server.URL, "phase": phase})
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, python, filepath.Join(packagePath, "tests/http_n8n_sources.py"))
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(packagePath, "src"), "STASH_API_KEY=fixture-application-key", "STASH_INGEST_TOKEN="+token)
		command.Stdin = bytes.NewReader(setup)
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		require.Contains(t, string(output), `"verified": true`)
	}
	run("lost_policy_reply")
	require.True(t, dropped.Load())
	require.EqualValues(t, 7, writes.Load(), "six collections and the first policy were saved")
	run("resume")
	require.EqualValues(t, 12, writes.Load(), "retry creates only the five missing policies")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		collections, err := repo.SourceCollection.List(ctx, "", 20)
		require.NoError(t, err)
		require.Len(t, collections, 6)
		for _, collection := range collections {
			require.Nil(t, collection.AccountUUID, "registration cannot invent account ownership")
			policy, err := repo.MetadataPolicy.Find(ctx, collection.UUID)
			require.NoError(t, err)
			require.True(t, policy.Definition.Enabled)
			require.False(t, policy.Definition.ApplyToScans)
			require.Equal(t, collection.Revision, policy.CollectionRevision)
		}
		_, _, err = db.ExecSQL(ctx, `INSERT INTO performers(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),(2,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id,name,position) VALUES(1,'A different display name',0),(2,'example',0);`, nil)
		require.NoError(t, err)
		owner, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, 1)
		require.NoError(t, err)
		account, err := repo.SourceAccount.Create(ctx, "native:reddit", "Linked account")
		require.NoError(t, err)
		now := time.Now()
		for i := 0; i < 9; i++ {
			name := fmt.Sprintf("otherhandle%d", i)
			if i == 0 {
				name = "example"
			}
			_, err = repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: name},
				models.AccountIdentifierEvidence{Key: name, Basis: "source-profile", Origin: "fixture", FirstObserved: now, LastObserved: now})
			require.NoError(t, err)
		}
		account, err = repo.SourceAccount.Find(ctx, account.UUID)
		require.NoError(t, err)
		_, err = repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
			State: models.AccountOwnershipLinked, PerformerUUID: owner.UUID, ExpectedPerformerRevision: owner.Revision, Origin: "review", Reason: "Explicit link"})
		return err
	}))
	run("linked_removal")
	require.EqualValues(t, 18, writes.Load(), "six sources disabled; completed request replay makes no writes")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		collections, err := repo.SourceCollection.List(ctx, "", 20)
		require.NoError(t, err)
		for _, collection := range collections {
			require.Equal(t, "disabled", collection.State)
		}
		return nil
	}))
}
