package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

const MaxAccountProfileBytes = 128 << 10

type AccountProfileEvent struct {
	Protocol           int             `json:"protocol"`
	ProducerUUID       string          `json:"producer_uuid"`
	EventUUID          string          `json:"event_uuid"`
	RunUUID            string          `json:"run_uuid"`
	CollectionUUID     string          `json:"collection_uuid"`
	CollectionRevision int             `json:"collection_revision"`
	RootUUID           *string         `json:"root_uuid"`
	OwnerUUID          string          `json:"owner_uuid"`
	Fence              int64           `json:"fence"`
	Kind               string          `json:"kind"`
	ObservedAt         time.Time       `json:"observed_at"`
	ExtractorVersion   string          `json:"extractor_version"`
	RetentionPolicy    string          `json:"retention_policy"`
	Source             json.RawMessage `json:"source"`
}

func (s *Service) AccountProfile(ctx context.Context, token string, raw []byte, digest string) (*models.IngestReceipt, error) {
	if _, err := s.Authenticate(ctx, token); err != nil {
		return nil, err
	}
	if Digest(raw) != digest {
		return nil, ErrInvalid
	}
	var event AccountProfileEvent
	if err := StrictJSON(raw, MaxAccountProfileBytes, &event); err != nil {
		return nil, err
	}
	for _, id := range []string{event.ProducerUUID, event.EventUUID, event.RunUUID, event.CollectionUUID, event.OwnerUUID} {
		if !ValidUUID(id) {
			return nil, ErrInvalid
		}
	}
	if event.RootUUID != nil && !ValidUUID(*event.RootUUID) {
		return nil, ErrInvalid
	}
	var receipt *models.IngestReceipt
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if credential.ProducerUUID != event.ProducerUUID {
			return ErrForbidden
		}
		receipt, err = s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.EventUUID)
		if err != nil {
			return err
		}
		if receipt != nil {
			if !permitted(credential, receipt.CollectionUUID, receipt.RootUUID) {
				return ErrForbidden
			}
			if receipt.Digest != digest {
				return models.ErrIngestReplay
			}
			return nil
		}
		if !permitted(credential, event.CollectionUUID, event.RootUUID) {
			return ErrForbidden
		}
		if event.Kind != "account.profile" || event.Protocol != ProtocolVersion || event.RetentionPolicy != archive.SourceRetentionVersion {
			return ErrUnsupported
		}
		if event.CollectionRevision < 1 || event.Fence < 1 || event.Fence > 1<<53-1 || event.ObservedAt.IsZero() || event.ObservedAt.UTC().Year() < 1 || event.ObservedAt.UTC().Year() > 9999 || len(event.ExtractorVersion) < 1 || len(event.ExtractorVersion) > 128 || strings.ContainsAny(event.ExtractorVersion, "\r\n\x00") {
			return ErrInvalid
		}
		retained, err := archive.RetainSourcePayload(event.Source)
		if err != nil {
			return ErrInvalid
		}
		tree, err := archive.DecodeJSONObject(event.Source, 65536)
		if err != nil {
			return ErrInvalid
		}
		canonical, err := archive.EncodeSourceJSON(tree)
		if err != nil || !bytes.Equal(canonical, retained) {
			return ErrInvalid
		}
		profile, err := sourceadapter.ExtractAccountProfile(retained)
		if err != nil {
			return ErrInvalid
		}
		run, err := s.Repo.SourceRun.Find(ctx, event.RunUUID)
		if err != nil {
			return err
		}
		if run == nil || run.Operation != "download" || run.CollectionUUID != event.CollectionUUID || run.CollectionRevision != event.CollectionRevision || !reflect.DeepEqual(run.RootUUID, event.RootUUID) {
			return ErrForbidden
		}
		attempts, err := s.Repo.SourceRun.Attempts(ctx, event.RunUUID, event.Fence-1, 1)
		if err != nil {
			return err
		}
		if len(attempts) != 1 || attempts[0].Fence != event.Fence || attempts[0].ProducerUUID != event.ProducerUUID || attempts[0].OwnerUUID != event.OwnerUUID {
			return ErrForbidden
		}
		collection, err := s.Repo.SourceCollection.Find(ctx, event.CollectionUUID)
		if err != nil {
			return err
		}
		if collection == nil || collection.State != "active" || event.CollectionRevision > collection.Revision {
			return ErrDefinition
		}
		if collection.Revision != event.CollectionRevision {
			history, err := s.Repo.SourceCollection.History(ctx, collection.UUID, event.CollectionRevision-1, 1)
			if err != nil {
				return err
			}
			if len(history) != 1 || history[0].Revision != event.CollectionRevision {
				return ErrDefinition
			}
			collection = &history[0].SourceCollection
		}
		if collection.State != "active" || collection.Kind != "account" || (collection.Namespace != "" && collection.Namespace != "native:reddit") || !reflect.DeepEqual(collection.RootUUID, event.RootUUID) {
			return ErrDefinition
		}
		target := scrape.RedditProfileURL(collection.TargetURL)
		if target == "" || sourceadapter.ProfileURLKey(target) != sourceadapter.ProfileURLKey(profile.URL) {
			return ErrForbidden
		}
		if event.RootUUID != nil {
			root, err := s.Repo.MediaRoot.Find(ctx, *event.RootUUID)
			if err != nil {
				return err
			}
			if root == nil || root.State != "active" {
				return ErrDefinition
			}
		}
		result, err := s.Repo.SourceAccount.RecordProfile(ctx, models.AccountProfileInput{
			UUID:         uuid.NewSHA1(uuid.MustParse(event.ProducerUUID), []byte("account.profile\x00"+event.EventUUID)).String(),
			ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision,
			ObservedAt: event.ObservedAt, ExtractorVersion: event.ExtractorVersion, Source: retained})
		if err != nil {
			return err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		receipt, err = s.Repo.Ingest.RecordReceipt(ctx, models.IngestReceipt{ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, Digest: digest,
			CredentialUUID: credential.UUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision, RootUUID: event.RootUUID, RunUUID: event.RunUUID, Kind: event.Kind, Result: body})
		return err
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}
