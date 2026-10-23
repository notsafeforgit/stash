package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/session"
)

func shareResult(ctx context.Context, repo models.Repository, row *models.ShareRecord) (*MediaShare, error) {
	var entries []models.ShareEntry
	err := repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		entries, err = repo.Share.SnapshotEntries(ctx, row.ID, 0, 3, "")
		return err
	})
	if err != nil {
		return nil, err
	}
	count := row.MediaCount
	ret := &MediaShare{ID: row.ID, Label: row.Label, CreatedAt: time.Unix(row.CreatedAt, 0), ExpiresAt: time.Unix(row.ExpiresAt, 0), AllowDownload: row.AllowDownload, ShowMetadata: row.ShowMetadata, AccessCount: row.AccessCount, MediaCount: &count, Items: []*MediaShareEntry{}, Mode: ShareMembershipSnapshot, SourceKind: ShareSourceKindSelection, PreparationState: SharePreparationState(row.PreparationState), PreparedCount: row.PreparedCount, PreparationError: row.PreparationError, SelectionCount: row.SelectionCount, MediaTypes: []ShareEntityKind{}}
	source, err := sharing.Source(row)
	if err != nil {
		return nil, err
	}
	if source != nil {
		ret.Mode = ShareMembership(source.Mode)
		if source.Mode == "ONGOING" {
			ret.MediaCount = nil
		}
		if source.Filter != nil {
			ret.SourceKind = ShareSourceKindFilter
			ret.MediaTypes = append(ret.MediaTypes, ShareEntityKind(source.Filter.Kind))
		} else if source.Entity != nil {
			ret.SourceKind = ShareSourceKind(source.Entity.Kind)
			ret.SourceName = source.Entity.Name
			for _, kind := range source.MediaTypes {
				ret.MediaTypes = append(ret.MediaTypes, ShareEntityKind(kind))
			}
		}
	}
	if secret := sharing.SavedLinkSecret(row); secret != "" {
		link := shareLink(ctx, row.ID, secret)
		ret.URL = &link
	}
	if row.RevokedAt != 0 {
		t := time.Unix(row.RevokedAt, 0)
		ret.RevokedAt = &t
	}
	if row.LastAccessedAt != 0 {
		t := time.Unix(row.LastAccessedAt, 0)
		ret.LastAccessedAt = &t
	}
	for _, entry := range entries {
		var id *string
		if entry.UUID != "" {
			id = &entry.UUID
		}
		ret.Items = append(ret.Items, &MediaShareEntry{Kind: ShareEntityKind(entry.Kind), ID: id, Title: entry.Title, MediaCount: entry.MediaCount})
	}
	return ret, nil
}

func shareLink(ctx context.Context, id, secret string) string {
	base := config.GetInstance().GetSharingPublicURL()
	if base == "" {
		base, _ = ctx.Value(BaseURLCtxKey).(string)
		base = strings.TrimRight(base, "/") + "/share"
	}
	return base + "/" + url.PathEscape(id) + "/?" + url.Values{"key": {secret}}.Encode()
}

func (r *queryResolver) MediaShares(ctx context.Context, limit int, offset int, status *MediaShareStatus) ([]*MediaShare, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, fmt.Errorf("invalid share pagination")
	}
	ret := []*MediaShare{}
	options := models.ShareListOptions{Limit: limit, Offset: offset, Now: time.Now().Unix()}
	if status != nil {
		active := *status == MediaShareStatusActive
		options.Active = &active
	}
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		rows, err := r.repository.Share.List(ctx, options)
		if err != nil {
			return err
		}
		for _, row := range rows {
			result, err := shareResult(ctx, r.repository, row)
			if err != nil {
				return err
			}
			ret = append(ret, result)
		}
		return nil
	})
	return ret, err
}

func (r *queryResolver) MediaShare(ctx context.Context, id string) (*MediaShare, error) {
	row, err := r.shares.Find(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	return shareResult(ctx, r.repository, row)
}

func (r *queryResolver) SharingConfiguration(ctx context.Context) (*SharingConfiguration, error) {
	return sharingConfiguration(r.settings.config), nil
}

func sharingConfiguration(c *config.Config) *SharingConfiguration {
	return &SharingConfiguration{PublicURL: c.GetSharingPublicURL(), MaxDays: 30}
}

func (r *mutationResolver) ConfigureSharing(ctx context.Context, publicURL string) (*SharingConfiguration, error) {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if err := config.ValidateSharingPublicURL(publicURL); err != nil {
		return nil, err
	}
	if err := r.settings.update(ctx, func(c *config.Config) (func(), error) {
		c.SetString(config.SharingPublicURL, publicURL)
		return nil, nil
	}); err != nil {
		return nil, err
	}
	return sharingConfiguration(r.settings.config), nil
}

func (r *mutationResolver) MediaShareCreate(ctx context.Context, input MediaShareCreateInput) (*MediaShareCreated, error) {
	targets := make([]sharing.Target, 0, len(input.Targets))
	for _, target := range input.Targets {
		id, err := strconv.Atoi(target.ID)
		if err != nil {
			return nil, fmt.Errorf("invalid share target")
		}
		targets = append(targets, sharing.Target{Kind: string(target.Kind), ID: id})
	}
	var source *models.ShareSource
	if input.Source != nil {
		source = &models.ShareSource{Mode: string(input.Source.Mode)}
		for _, kind := range input.Source.MediaTypes {
			source.MediaTypes = append(source.MediaTypes, string(kind))
		}
		if filter := input.Source.Filter; filter != nil {
			source.Filter = &models.ShareFilter{Kind: string(filter.Kind), AST: filter.FilterAst}
			if filter.Q != nil {
				source.Filter.Q = *filter.Q
			}
		}
		if entity := input.Source.Entity; entity != nil {
			id, err := strconv.Atoi(entity.ID)
			if err != nil {
				return nil, err
			}
			uuid, err := entityid.Public(ctx, models.ArchiveEntityKind(strings.ToLower(string(entity.Kind))), id)
			if err != nil {
				return nil, err
			}
			source.Entity = &models.ShareEntitySource{Kind: string(entity.Kind), UUID: uuid}
		}
	}
	owner := "owner"
	if user := session.GetCurrentUserID(ctx); user != nil && *user != "" {
		owner = *user
	}
	row, _, err := r.shares.CreateFromSource(ctx, owner, sharing.Options{Label: input.Label, ExpiresAt: input.ExpiresAt, AllowDownload: input.AllowDownload, ShowMetadata: input.ShowMetadata}, targets, source)
	if err != nil {
		return nil, err
	}
	result, err := shareResult(ctx, r.repository, row)
	if err != nil {
		return nil, err
	}
	return &MediaShareCreated{Share: result, URL: result.URL}, nil
}

func (r *mutationResolver) MediaShareUpdate(ctx context.Context, input MediaShareUpdateInput) (*MediaShare, error) {
	row, err := r.shares.Update(ctx, input.ID, sharing.Options{Label: input.Label, ExpiresAt: input.ExpiresAt, AllowDownload: input.AllowDownload, ShowMetadata: input.ShowMetadata})
	if err != nil {
		return nil, err
	}
	return shareResult(ctx, r.repository, row)
}

func (r *mutationResolver) MediaShareRevoke(ctx context.Context, id string) (bool, error) {
	err := r.shares.Revoke(ctx, id)
	return err == nil, err
}
func (r *mutationResolver) MediaShareDelete(ctx context.Context, id string) (bool, error) {
	err := r.shares.Delete(ctx, id)
	return err == nil, err
}
func (r *mutationResolver) MediaSharesDeleteInactive(ctx context.Context) (int, error) {
	return r.shares.DeleteInactive(ctx)
}
func (r *mutationResolver) MediaShareRotate(ctx context.Context, id string) (string, error) {
	secret, err := r.shares.Rotate(ctx, id)
	if err != nil {
		return "", err
	}
	return shareLink(ctx, id, secret), nil
}

func (r *mutationResolver) MediaShareRetry(ctx context.Context, id string) (*MediaShare, error) {
	row, err := r.shares.Retry(ctx, id)
	if err != nil {
		return nil, err
	}
	return shareResult(ctx, r.repository, row)
}
