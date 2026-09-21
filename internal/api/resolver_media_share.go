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
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/session"
)

func shareResult(row *models.ShareRecord) (*MediaShare, error) {
	snapshot, err := sharing.Snapshot(row)
	if err != nil {
		return nil, err
	}
	ret := &MediaShare{ID: row.ID, Label: row.Label, CreatedAt: time.Unix(row.CreatedAt, 0), ExpiresAt: time.Unix(row.ExpiresAt, 0), AllowDownload: row.AllowDownload, ShowMetadata: row.ShowMetadata, AccessCount: row.AccessCount, MediaCount: len(snapshot.Media), Items: []*MediaShareEntry{}}
	if row.RevokedAt != 0 {
		t := time.Unix(row.RevokedAt, 0)
		ret.RevokedAt = &t
	}
	if row.LastAccessedAt != 0 {
		t := time.Unix(row.LastAccessedAt, 0)
		ret.LastAccessedAt = &t
	}
	for _, entry := range snapshot.Entries {
		ret.Items = append(ret.Items, &MediaShareEntry{Kind: ShareEntityKind(entry.Kind), ID: strconv.Itoa(entry.ID), Title: entry.Title, MediaCount: len(entry.MediaKeys)})
	}
	return ret, nil
}

func shareLink(ctx context.Context, id, secret string) string {
	base := config.GetInstance().GetSharingPublicURL()
	if base == "" {
		base, _ = ctx.Value(BaseURLCtxKey).(string)
		base = strings.TrimRight(base, "/") + "/share"
	}
	return base + "/" + url.PathEscape(id) + "/#" + secret
}

func (r *queryResolver) MediaShares(ctx context.Context, limit int, offset int) ([]*MediaShare, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, fmt.Errorf("invalid share pagination")
	}
	ret := []*MediaShare{}
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		rows, err := r.repository.Share.List(ctx, limit, offset)
		if err != nil {
			return err
		}
		for _, row := range rows {
			result, err := shareResult(row)
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
	return shareResult(row)
}

func (r *queryResolver) SharingConfiguration(ctx context.Context) (*SharingConfiguration, error) {
	return &SharingConfiguration{PublicURL: config.GetInstance().GetSharingPublicURL(), MaxItems: sharing.MaxItems, MaxDays: 30}, nil
}

func (r *mutationResolver) ConfigureSharing(ctx context.Context, publicURL string) (*SharingConfiguration, error) {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if err := config.ValidateSharingPublicURL(publicURL); err != nil {
		return nil, err
	}
	c := config.GetInstance()
	c.SetString(config.SharingPublicURL, publicURL)
	if err := c.Write(); err != nil {
		return nil, err
	}
	return &SharingConfiguration{PublicURL: publicURL, MaxItems: sharing.MaxItems, MaxDays: 30}, nil
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
	owner := "owner"
	if user := session.GetCurrentUserID(ctx); user != nil && *user != "" {
		owner = *user
	}
	row, secret, err := r.shares.Create(ctx, owner, sharing.Options{Label: input.Label, ExpiresAt: input.ExpiresAt, AllowDownload: input.AllowDownload, ShowMetadata: input.ShowMetadata}, targets)
	if err != nil {
		return nil, err
	}
	result, err := shareResult(row)
	if err != nil {
		return nil, err
	}
	return &MediaShareCreated{Share: result, URL: shareLink(ctx, row.ID, secret)}, nil
}

func (r *mutationResolver) MediaShareUpdate(ctx context.Context, input MediaShareUpdateInput) (*MediaShare, error) {
	row, err := r.shares.Update(ctx, input.ID, sharing.Options{Label: input.Label, ExpiresAt: input.ExpiresAt, AllowDownload: input.AllowDownload, ShowMetadata: input.ShowMetadata})
	if err != nil {
		return nil, err
	}
	return shareResult(row)
}

func (r *mutationResolver) MediaShareRevoke(ctx context.Context, id string) (bool, error) {
	err := r.shares.Revoke(ctx, id)
	return err == nil, err
}
func (r *mutationResolver) MediaShareRotate(ctx context.Context, id string) (string, error) {
	secret, err := r.shares.Rotate(ctx, id)
	if err != nil {
		return "", err
	}
	return shareLink(ctx, id, secret), nil
}
func (r *mutationResolver) MediaSharePreview(ctx context.Context, id string) (string, error) {
	secret, err := r.shares.Preview(ctx, id)
	if err != nil {
		return "", err
	}
	return shareLink(ctx, id, secret), nil
}
