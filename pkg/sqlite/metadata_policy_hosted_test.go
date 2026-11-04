package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
	"github.com/stretchr/testify/require"
)

const hostedRedgifsCapture = `{"category":"redgifs","id":"HostedClip","description":"Redgifs caption","createDate":1704067200,"urls":{"hd":"https://media.redgifs.com/HostedClip.mp4"}}`
const hostingRedditCapture = `{"category":"reddit","id":"reddit1","title":"Reddit title","selftext":"Reddit details","date":"2024-02-01T00:00:00Z","url":"https://www.redgifs.com/watch/HostedClip"}`

func hostedPolicySource(t *testing.T, f metadataPolicyFixture, raw string) *metadata.Source {
	t.Helper()
	retained, err := archive.RetainSourcePayload([]byte(raw))
	require.NoError(t, err)
	ref, err := sourceadapter.ExtractCapturedPost(retained)
	require.NoError(t, err)
	post := sourceTestPost(t, f.repo, *ref, "")
	platform := strings.TrimPrefix(ref.Namespace, "native:")
	payload, err := archive.PrepareRetainedCapture("gallery-dl", platform, retained)
	require.NoError(t, err)
	fields, err := archive.CapturedMetadata(retained)
	require.NoError(t, err)
	capture := recordSourceTestCapture(t, f.repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
		Origin: "gallery-dl", Platform: platform, CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion,
		Metadata: fields, Payload: *payload})
	album, err := sourceadapter.ExtractCapturedAlbum(retained)
	require.NoError(t, err)
	require.NotNil(t, album)
	album.Manifest.CaptureUUID = capture.UUID
	manifest := recordAttachmentManifest(t, f.repo, album.Manifest)
	attachment := manifestEntries(t, f.repo, manifest.UUID)[0].Attachment
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: f.collection.UUID,
			CollectionRevision: f.collection.Revision, CaptureUUID: capture.UUID})
	}))
	entity := metadataState(t, f.repo, f.entity.UUID, "title").Entity
	require.NoError(t, applyMediaChoice(f.repo, models.AttachmentMediaDecisionInput{AttachmentUUID: attachment.UUID,
		ExpectedAttachmentRevision: attachment.Revision, State: "linked", MediaUUID: entity.UUID,
		ExpectedMediaRevision: entity.Revision, Origin: "review"}))
	return &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID}
}

func hostedPolicy(t *testing.T, f metadataPolicyFixture) *models.MetadataPolicy {
	t.Helper()
	var result *models.MetadataPolicy
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID,
			ExpectedCollectionRevision: f.collection.Revision, Origin: "review", Definition: models.MetadataPolicyDefinition{
				Enabled: true, Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{f.entity.Kind: {
					OnCreate: true, OnExisting: true, Mappings: map[string]models.MetadataMapping{
						"title":   {JQ: `.source.metadata.title // .source.metadata.original_text // empty`},
						"details": {JQ: `.source.metadata.original_text // empty`},
						"date":    {JQ: `.source.metadata.published_at[:10]`},
					},
				}},
			}})
		return err
	}))
	return result
}

func TestMetadataPolicyPrefersRedditForItsHostedRedgifsItem(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage} {
		for _, redditFirst := range []bool{false, true} {
			name := string(kind) + "/redgifs-first"
			if redditFirst {
				name = string(kind) + "/reddit-first"
			}
			t.Run(name, func(t *testing.T) {
				f := newMetadataPolicyFixture(t)
				if kind == models.ArchiveImage {
					image := models.NewImage()
					require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
						return f.repo.Image.Create(ctx, &models.CreateImageInput{Image: &image})
					}))
					f.entity = archiveFind(t, f.repo, kind, image.ID)
				}
				reddit := f
				reddit.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
					Label: "Reddit", Kind: "directory", State: "active", RootUUID: &f.root.UUID, PathPrefix: ".",
				}})
				hostInput, redditInput := f.input(hostedPolicy(t, f)), reddit.input(hostedPolicy(t, reddit))
				hostInput.Source = hostedPolicySource(t, f, hostedRedgifsCapture)
				redditInput.Source = hostedPolicySource(t, reddit, hostingRedditCapture)
				order := []metadata.Input{hostInput, redditInput}
				if redditFirst {
					order[0], order[1] = order[1], order[0]
				}
				for _, input := range order {
					result, err := applyPolicy(f.repo, input, "")
					require.NoError(t, err)
					require.Empty(t, result.ReviewFields())
				}
				for field, value := range map[string]string{"title": `"Reddit title"`, "details": `"Reddit details"`, "date": `"2024-02-01"`} {
					state := metadataState(t, f.repo, f.entity.UUID, field)
					require.JSONEq(t, value, string(state.Value))
					require.Equal(t, redditInput.Source.CaptureUUID, *state.Decision.CaptureUUID)
				}
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
				for _, input := range []metadata.Input{hostInput, redditInput, hostInput} {
					result, err := applyPolicy(f.repo, input, "")
					require.NoError(t, err)
					require.Empty(t, result.AppliedFields())
				}
			})
		}
	}
}

func TestMetadataPolicyHostedPreferencePreservesUnrelatedAndManualChoices(t *testing.T) {
	for _, scenario := range []string{"different-hosted-item", "manual-title", "manual-clear"} {
		t.Run(scenario, func(t *testing.T) {
			f := newMetadataPolicyFixture(t)
			policy := hostedPolicy(t, f)
			first := f.input(policy)
			first.Source = hostedPolicySource(t, f, hostedRedgifsCapture)
			_, err := applyPolicy(f.repo, first, "")
			require.NoError(t, err)
			if scenario != "different-hosted-item" {
				mode, value := "set", `"My title"`
				if scenario == "manual-clear" {
					mode, value = "clear", ""
				}
				_, err := applyMetadata(f.repo, metadataInput(metadataState(t, f.repo, f.entity.UUID, "title"), mode, "review", value), false)
				require.NoError(t, err)
			}
			before := metadataState(t, f.repo, f.entity.UUID, "title")
			raw := hostingRedditCapture
			if scenario == "different-hosted-item" {
				raw = strings.ReplaceAll(raw, "HostedClip", "DifferentClip")
			}
			next := f.input(policy)
			next.Source = hostedPolicySource(t, f, raw)
			_, err = applyPolicy(f.repo, next, "")
			require.NoError(t, err)
			after := metadataState(t, f.repo, f.entity.UUID, "title")
			require.Equal(t, before.Value, after.Value)
			require.Equal(t, before.Decision, after.Decision)
		})
	}
}
