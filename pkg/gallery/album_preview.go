package gallery

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// The application view deliberately exposes one preview signature and portable
// identities. Repository-only fields and their Go names are not the API schema.
type AlbumIdentity struct {
	UUID     string                    `json:"uuid"`
	Kind     models.ArchiveEntityKind  `json:"kind"`
	State    models.ArchiveEntityState `json:"state"`
	Revision int                       `json:"revision"`
	LocalID  *int                      `json:"local_id,omitempty"`
}

type AlbumEntry struct {
	Position       int                      `json:"position"`
	AttachmentUUID string                   `json:"attachment_uuid"`
	MediaUUID      *string                  `json:"media_uuid,omitempty"`
	MediaKind      models.ArchiveEntityKind `json:"media_kind,omitempty"`
	MediaRevision  int                      `json:"media_revision,omitempty"`
	Status         string                   `json:"status"`
}

type AlbumReference struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type AlbumMatch struct {
	models.SourceAlbumMatch
	Reference AlbumReference `json:"reference"`
}

type AlbumAssociation struct {
	UUID     string `json:"uuid"`
	State    string `json:"state"`
	Revision int    `json:"revision"`
	Origin   string `json:"origin"`
	Reason   string `json:"reason,omitempty"`
}

type AlbumMetadata struct {
	Title   string  `json:"title"`
	Details string  `json:"details"`
	Date    *string `json:"date"`
}

type AlbumPreview struct {
	PostUUID        string            `json:"post_uuid"`
	Policy          string            `json:"policy"`
	Signature       string            `json:"signature"`
	Action          string            `json:"action"`
	SelectionUUID   string            `json:"selection_uuid,omitempty"`
	Gallery         *AlbumIdentity    `json:"gallery,omitempty"`
	Association     *AlbumAssociation `json:"association,omitempty"`
	InitialMetadata *AlbumMetadata    `json:"initial_metadata,omitempty"`
	Entries         []AlbumEntry      `json:"entries"`
	Add             []AlbumIdentity   `json:"add"`
	Remove          []AlbumIdentity   `json:"remove"`
	Matches         []AlbumMatch      `json:"matches"`
}

func albumIdentity(entity *models.ArchiveEntity) *AlbumIdentity {
	if entity == nil {
		return nil
	}
	return &AlbumIdentity{UUID: entity.UUID, Kind: entity.Kind, State: entity.State, Revision: entity.Revision, LocalID: entity.LocalID}
}

func albumPreview(input *models.SourceAlbumBackfillPreview) *AlbumPreview {
	g := input.Gallery
	ret := &AlbumPreview{PostUUID: input.PostUUID, Policy: input.Policy, Signature: input.Signature, Action: g.Action, SelectionUUID: g.SelectionUUID, Gallery: albumIdentity(g.Gallery),
		Entries: []AlbumEntry{}, Add: []AlbumIdentity{}, Remove: []AlbumIdentity{}, Matches: []AlbumMatch{}}
	if g.Association != nil {
		a := g.Association
		ret.Association = &AlbumAssociation{UUID: a.UUID, State: a.State, Revision: a.Revision, Origin: a.Origin, Reason: a.Reason}
	}
	if g.Action == "create" {
		ret.InitialMetadata = &AlbumMetadata{Title: g.Title, Details: g.Details}
		if g.Date != nil {
			date := g.Date.String()
			ret.InitialMetadata.Date = &date
		}
	}
	for _, e := range g.Entries {
		ret.Entries = append(ret.Entries, AlbumEntry{Position: e.Position, AttachmentUUID: e.AttachmentUUID, MediaUUID: e.MediaUUID, MediaKind: e.MediaKind, MediaRevision: e.MediaRevision, Status: e.Status})
	}
	for _, m := range g.Add {
		ret.Add = append(ret.Add, *albumIdentity(&m))
	}
	for _, m := range g.Remove {
		ret.Remove = append(ret.Remove, *albumIdentity(&m))
	}
	for _, m := range input.Matches {
		ret.Matches = append(ret.Matches, AlbumMatch{SourceAlbumMatch: m, Reference: AlbumReference{Namespace: m.Reference.Namespace, Value: m.Reference.Value}})
	}
	return ret
}

func (s *AlbumBackfill) Preview(ctx context.Context, post, policy string) (*AlbumPreview, error) {
	if !albumUUID(post) || !models.ValidSourceAlbumPolicy(policy) {
		return nil, ErrAlbumWorkInvalid
	}
	var preview *models.SourceAlbumBackfillPreview
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		parent, err := s.Durable.Repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if parent == nil {
			return ErrAlbumWorkNotFound
		}
		preview, err = s.Durable.Repo.SourceGallery.PreviewBackfill(ctx, post, policy)
		return err
	})
	if err != nil {
		return nil, err
	}
	return albumPreview(preview), nil
}
