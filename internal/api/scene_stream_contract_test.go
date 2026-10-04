package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/stashapp/stash/internal/api/loaders"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/signedurl"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSceneStreamNativeAPIContract(t *testing.T) {
	cfg := config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty(); manager.SetInstance(nil) })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "streams.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	p := paths.NewPaths(t.TempDir(), "")
	manager.SetInstance(&manager.Manager{Config: cfg, Paths: &p})
	cfg.SetString(config.MaxStreamingTranscodeSize, string(models.StreamingResolutionEnumStandardHd))
	cfg.SetString(config.JWTSignKey, "native-stream-test-key")
	scene, empty := models.NewScene(), models.NewScene()
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
		folder := &models.Folder{Path: t.TempDir()}
		if err := repo.Folder.Create(ctx, folder); err != nil {
			return err
		}
		file := &models.VideoFile{
			BaseFile: &models.BaseFile{ParentFolderID: folder.ID, Basename: "stream.webm", Path: filepath.Join(folder.Path, "stream.webm")},
			Format:   string(ffmpeg.Webm), VideoCodec: ffmpeg.Vp9, AudioCodec: "opus", Width: 1920, Height: 1080,
		}
		if err := repo.File.Create(ctx, file); err != nil {
			return err
		}
		if err := repo.Scene.Create(ctx, &scene, []models.FileID{file.ID}); err != nil {
			return err
		}
		return repo.Scene.Create(ctx, &empty, nil)
	}))
	server := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{repository: repo}}))
	server.AddTransport(transport.POST{})
	middleware := loaders.Middleware{Repository: repo}
	api := middleware.Middleware(server)
	type response struct {
		Data struct {
			Scene *struct {
				Streams []*manager.SceneStreamEndpoint `json:"sceneStreams"`
			} `json:"findScene"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	request := func(ctx context.Context, query, id string) response {
		t.Helper()
		body, err := json.Marshal(map[string]any{"query": query, "variables": map[string]any{"id": id}})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		api.ServeHTTP(recorder, req)
		require.Contains(t, []int{http.StatusOK, http.StatusUnprocessableEntity}, recorder.Code, recorder.Body.String())
		var result response
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		return result
	}
	const query = `query($id: ID!) { findScene(id: $id) { sceneStreams { url mime_type label } } }`
	ctx := context.WithValue(testCtx, BaseURLCtxKey, "https://stash.example/prefix")
	id := strconv.Itoa(scene.ID)
	base := "/prefix/scene/" + id + "/stream"
	for _, auth := range []string{"anonymous", "api key", "signed user"} {
		t.Run(auth, func(t *testing.T) {
			cfg.SetString(config.ApiKey, "")
			cfg.SetString(config.Username, "")
			cfg.SetString(config.Password, "")
			requestCtx := ctx
			if auth != "anonymous" {
				cfg.SetString(config.ApiKey, "stream-test-api-key")
			}
			if auth == "signed user" {
				cfg.SetString(config.Username, "owner")
				cfg.SetPassword("test-password")
				requestCtx = session.SetCurrentUserID(ctx, "owner")
			}
			result := request(requestCtx, query, id)
			require.Empty(t, result.Errors)
			require.NotNil(t, result.Data.Scene)
			streams := result.Data.Scene.Streams
			require.Len(t, streams, 4, "direct playback plus 720p, 480p and 240p HLS")
			require.Equal(t, ffmpeg.MimeWebmVideo, *streams[0].MimeType)
			var resolutions []string
			for i, stream := range streams {
				u, err := url.Parse(stream.URL)
				require.NoError(t, err)
				require.Equal(t, "stash.example", u.Host)
				if i == 0 {
					require.Equal(t, base, u.Path)
				} else {
					require.Equal(t, base+".master.m3u8", u.Path)
					require.Equal(t, ffmpeg.MimeHLS, *stream.MimeType)
					resolutions = append(resolutions, u.Query().Get("resolution"))
				}
				switch auth {
				case "signed user":
					require.Empty(t, u.Query().Get("apikey"))
					cid, err := signedurl.VerifyURL(u.Path, u.Query(), cfg.GetJWTSignKey())
					require.NoError(t, err)
					require.Equal(t, signedurl.GenerateCredentialID(cfg.GetJWTSignKey(), "owner"), cid)
					_, err = signedurl.VerifyURL("/prefix/scene/999/stream", u.Query(), cfg.GetJWTSignKey())
					require.Error(t, err, "signatures must not authorize another scene")
				case "api key":
					require.Equal(t, "stream-test-api-key", u.Query().Get("apikey"))
					require.Empty(t, u.Query().Get(signedurl.SigParam))
				default:
					require.Empty(t, u.Query().Get("apikey"))
					require.Empty(t, u.Query().Get(signedurl.SigParam))
				}
			}
			require.Equal(t, []string{"STANDARD_HD", "STANDARD", "LOW"}, resolutions)
		})
	}
	require.NotEmpty(t, request(ctx, query, id).Errors, "configured authentication requires a user context")
	userCtx := session.SetCurrentUserID(ctx, "owner")
	result := request(userCtx, query, strconv.Itoa(empty.ID))
	require.Empty(t, result.Errors)
	require.NotNil(t, result.Data.Scene)
	require.Empty(t, result.Data.Scene.Streams)
	for _, retired := range []string{
		`query($id: ID!) { sceneStreams(id: $id) { url } }`,
		`query($id: ID!) { findScene(id: $id) { sceneStreamsV3 { url } } }`,
	} {
		require.NotEmpty(t, request(userCtx, retired, id).Errors)
	}
}
