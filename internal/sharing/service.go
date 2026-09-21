// Package sharing owns anonymous, narrowly scoped, revocable media grants.
package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

const MaxItems = 2000

var ErrUnavailable = errors.New("share unavailable")

type Target struct {
	Kind string
	ID   int
}
type Options struct {
	Label                       string
	ExpiresAt                   time.Time
	AllowDownload, ShowMetadata bool
}
type Service struct {
	Repo   models.Repository
	mu     sync.Mutex
	active map[string]map[*delivery]struct{}
	// Set once during server construction. Releases only this grant's encoders.
	OnInvalidate func(string)
}
type delivery struct{ cancel context.CancelFunc }

func New(repo models.Repository) *Service {
	return &Service{Repo: repo, active: make(map[string]map[*delivery]struct{})}
}

func token(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Hash(secret string) []byte { h := sha256.Sum256([]byte(secret)); return h[:] }

func validSecret(secret string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(secret)
	return err == nil && len(b) == 32
}

func Active(s *models.ShareRecord, now time.Time) bool {
	return s != nil && s.RevokedAt == 0 && now.Unix() < s.ExpiresAt
}

func validateOptions(o Options) error {
	if len(strings.TrimSpace(o.Label)) == 0 || utf8.RuneCountInString(o.Label) > 200 {
		return errors.New("share label must contain 1–200 characters")
	}
	if !o.ExpiresAt.After(time.Now()) || o.ExpiresAt.After(time.Now().Add(30*24*time.Hour)) {
		return errors.New("share expiry must be in the next 30 days")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, owner string, o Options, targets []Target) (*models.ShareRecord, string, error) {
	if err := validateOptions(o); err != nil {
		return nil, "", err
	}
	if len(targets) == 0 || len(targets) > MaxItems {
		return nil, "", fmt.Errorf("select between 1 and %d items", MaxItems)
	}
	id, err := token(16)
	if err != nil {
		return nil, "", err
	}
	secret, err := token(32)
	if err != nil {
		return nil, "", err
	}
	row := &models.ShareRecord{ID: id, TokenHash: Hash(secret), Label: strings.TrimSpace(o.Label), CreatedBy: owner, CreatedAt: time.Now().Unix(), ExpiresAt: o.ExpiresAt.Unix(), AllowDownload: o.AllowDownload, ShowMetadata: o.ShowMetadata, Version: 1}
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		snapshot, err := s.snapshot(ctx, targets)
		if err != nil {
			return err
		}
		row.SnapshotJSON, err = json.Marshal(snapshot)
		if err != nil {
			return err
		}
		return s.Repo.Share.Create(ctx, row)
	})
	if err != nil {
		return nil, "", err
	}
	return row, secret, nil
}

func (s *Service) Find(ctx context.Context, id string) (ret *models.ShareRecord, err error) {
	err = s.Repo.WithReadTxn(ctx, func(ctx context.Context) error { ret, err = s.Repo.Share.Find(ctx, id); return err })
	return
}

func (s *Service) Update(ctx context.Context, id string, o Options) (*models.ShareRecord, error) {
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	var ret *models.ShareRecord
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ret, err = s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if ret == nil || ret.RevokedAt != 0 {
			return ErrUnavailable
		}
		ret.Label, ret.ExpiresAt, ret.AllowDownload, ret.ShowMetadata = strings.TrimSpace(o.Label), o.ExpiresAt.Unix(), o.AllowDownload, o.ShowMetadata
		return s.Repo.Share.Update(ctx, ret)
	})
	if err == nil {
		s.Cancel(id)
	}
	return ret, err
}

func (s *Service) Revoke(ctx context.Context, id string) error {
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err := s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return ErrUnavailable
		}
		row.RevokedAt = time.Now().Unix()
		row.Version++
		if err := s.Repo.Share.Update(ctx, row); err != nil {
			return err
		}
		return s.Repo.Share.DeleteSessions(ctx, id)
	})
	if err == nil {
		s.Cancel(id)
	}
	return err
}

func (s *Service) Rotate(ctx context.Context, id string) (string, error) {
	secret, err := token(32)
	if err != nil {
		return "", err
	}
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err := s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Active(row, time.Now()) {
			return ErrUnavailable
		}
		row.TokenHash = Hash(secret)
		row.Version++
		if err := s.Repo.Share.Update(ctx, row); err != nil {
			return err
		}
		return s.Repo.Share.DeleteSessions(ctx, id)
	})
	if err != nil {
		return "", err
	}
	s.Cancel(id)
	return secret, nil
}

// Preview issues a short-lived, single-use bootstrap secret. It does not rotate
// the recipient's link, and preview sessions do not increment access activity.
func (s *Service) Preview(ctx context.Context, id string) (string, error) {
	secret, err := token(32)
	if err != nil {
		return "", err
	}
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err := s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Active(row, time.Now()) {
			return ErrUnavailable
		}
		return s.Repo.Share.PutSession(ctx, &models.ShareSession{TokenHash: Hash(secret), ShareID: id, Version: row.Version, ExpiresAt: min(row.ExpiresAt, time.Now().Add(5*time.Minute).Unix()), Preview: true, ExchangeOnly: true})
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) Exchange(ctx context.Context, id, secret string) (string, *models.ShareRecord, error) {
	if !validSecret(secret) {
		return "", nil, ErrUnavailable
	}
	sessionToken, err := token(32)
	if err != nil {
		return "", nil, err
	}
	var row *models.ShareRecord
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err = s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Active(row, time.Now()) {
			return ErrUnavailable
		}
		hash := Hash(secret)
		preview := false
		if subtle.ConstantTimeCompare(row.TokenHash, hash) != 1 {
			bootstrap, err := s.Repo.Share.FindSession(ctx, hash)
			if err != nil {
				return err
			}
			if bootstrap == nil || !bootstrap.ExchangeOnly || !bootstrap.Preview || bootstrap.ShareID != id || bootstrap.Version != row.Version || bootstrap.ExpiresAt <= time.Now().Unix() {
				return ErrUnavailable
			}
			preview = true
			if err := s.Repo.Share.DeleteSession(ctx, hash); err != nil {
				return err
			}
		}
		expiresAt := row.ExpiresAt
		if preview {
			expiresAt = min(expiresAt, time.Now().Add(15*time.Minute).Unix())
		}
		if err := s.Repo.Share.PutSession(ctx, &models.ShareSession{TokenHash: Hash(sessionToken), ShareID: id, Version: row.Version, ExpiresAt: expiresAt, Preview: preview}); err != nil {
			return err
		}
		if !preview {
			return s.Repo.Share.RecordAccess(ctx, id, time.Now().Unix())
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return sessionToken, row, nil
}

func (s *Service) Authenticate(ctx context.Context, id, secret string) (*models.ShareRecord, error) {
	if !validSecret(secret) {
		return nil, ErrUnavailable
	}
	var row *models.ShareRecord
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		session, err := s.Repo.Share.FindSession(ctx, Hash(secret))
		if err != nil {
			return err
		}
		if session == nil || session.ExchangeOnly || session.ShareID != id || session.ExpiresAt <= time.Now().Unix() {
			return ErrUnavailable
		}
		row, err = s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Active(row, time.Now()) || session.Version != row.Version {
			return ErrUnavailable
		}
		// A response cannot outlive its guest session (including previews).
		row.ExpiresAt = min(row.ExpiresAt, session.ExpiresAt)
		return nil
	})
	return row, err
}

func Snapshot(row *models.ShareRecord) (models.ShareSnapshot, error) {
	var ret models.ShareSnapshot
	err := json.Unmarshal(row.SnapshotJSON, &ret)
	return ret, err
}

// BeginDelivery bounds concurrency and makes revocation interrupt responses
// already in progress. Callers revalidate after registration to close the race
// between initial authentication and a concurrent revocation/rotation.
func (s *Service) BeginDelivery(ctx context.Context, row *models.ShareRecord) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, deliveries := range s.active {
		total += len(deliveries)
	}
	if total >= 64 || len(s.active[row.ID]) >= 12 {
		return nil, nil, errors.New("share is busy")
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(row.ExpiresAt, 0))
	d := &delivery{cancel: cancel}
	if s.active[row.ID] == nil {
		s.active[row.ID] = make(map[*delivery]struct{})
	}
	s.active[row.ID][d] = struct{}{}
	return ctx, func() {
		cancel()
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.active[row.ID], d)
		if len(s.active[row.ID]) == 0 {
			delete(s.active, row.ID)
		}
	}, nil
}

func (s *Service) Cancel(id string) {
	s.mu.Lock()
	for d := range s.active[id] {
		d.cancel()
	}
	s.mu.Unlock()
	if s.OnInvalidate != nil {
		s.OnInvalidate(id)
	}
}
