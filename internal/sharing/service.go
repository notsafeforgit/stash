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
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

var ErrUnavailable = errors.New("share unavailable")
var ErrActive = errors.New("revoke an active share before deleting it")

type Target = models.ShareTarget
type Options struct {
	Label                       string
	ExpiresAt                   time.Time
	AllowDownload, ShowMetadata bool
}
type Service struct {
	Repo   models.Repository
	mu     sync.Mutex
	active map[string]map[*delivery]struct{}
	wake   chan struct{}
	// Set once during server construction. Releases only this grant's encoders.
	OnInvalidate func(string)
}
type delivery struct{ cancel context.CancelFunc }

func New(repo models.Repository) *Service {
	return &Service{Repo: repo, active: make(map[string]map[*delivery]struct{}), wake: make(chan struct{}, 1)}
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

// SavedLinkSecret is for authenticated management only. Older hash-only shares
// remain valid but have no retrievable URL until explicitly regenerated.
func Ready(row *models.ShareRecord, now time.Time) bool {
	return Active(row, now) && row.PreparationState == "READY"
}

func SavedLinkSecret(row *models.ShareRecord) string {
	if !Ready(row, time.Now()) || !validSecret(row.Token) || subtle.ConstantTimeCompare(row.TokenHash, Hash(row.Token)) != 1 {
		return ""
	}
	return row.Token
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
	return s.CreateFromSource(ctx, owner, o, targets, nil)
}

func (s *Service) CreateFromSource(ctx context.Context, owner string, o Options, targets []Target, source *models.ShareSource) (*models.ShareRecord, string, error) {
	if err := validateOptions(o); err != nil {
		return nil, "", err
	}
	if source == nil && len(targets) == 0 {
		return nil, "", errors.New("select at least one item")
	}
	id, err := token(16)
	if err != nil {
		return nil, "", err
	}
	secret, err := token(32)
	if err != nil {
		return nil, "", err
	}
	row := &models.ShareRecord{ID: id, TokenHash: Hash(secret), Token: secret, Label: strings.TrimSpace(o.Label), CreatedBy: owner, CreatedAt: time.Now().Unix(), ExpiresAt: o.ExpiresAt.Unix(), AllowDownload: o.AllowDownload, ShowMetadata: o.ShowMetadata, Version: 1, PreparationState: "PREPARING"}
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		if source != nil {
			if len(targets) != 0 {
				return errors.New("choose either selected items or a share source")
			}
			if err := s.validateSource(ctx, source); err != nil {
				return err
			}
			row.SourceJSON, err = json.Marshal(source)
			if err != nil {
				return err
			}
			if source.Mode == "ONGOING" {
				row.PreparationState = "READY"
				return s.Repo.Share.Create(ctx, row)
			}
		}
		var filters []models.ShareFilter
		if source != nil {
			filters, err = s.sourceQueries(ctx, source)
			if err != nil {
				return err
			}
		}
		if err := s.Repo.Share.Create(ctx, row); err != nil {
			return err
		}
		if err := s.Repo.Share.FreezeTargets(ctx, row.ID, targets, filters); err != nil {
			return err
		}
		return s.Repo.Share.FreezeMedia(ctx, row.ID)
	})
	if err != nil {
		return nil, "", err
	}
	row, err = s.Find(ctx, row.ID)
	if err != nil {
		return nil, "", err
	}
	// Small selections use the same durable checkpoint path as the worker.
	if row.PreparationState == "PREPARING" && row.MediaCount <= PreparationBatchSize {
		if err := s.PrepareBatch(ctx, row); err != nil {
			return nil, "", err
		}
		row, err = s.Find(ctx, row.ID)
		if err != nil {
			return nil, "", err
		}
	}
	s.Wake()
	return row, SavedLinkSecret(row), nil
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
		row.Token = ""
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

func (s *Service) DeleteInactive(ctx context.Context) (int, error) {
	var ids []string
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		ids, err = s.Repo.Share.DeleteInactive(ctx, time.Now().Unix())
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.Cancel(id)
	}
	return len(ids), nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		row, err := s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if row == nil {
			return nil
		}
		if Active(row, time.Now()) {
			return ErrActive
		}
		return s.Repo.Share.Delete(ctx, id)
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
		if !Ready(row, time.Now()) {
			return ErrUnavailable
		}
		row.TokenHash = Hash(secret)
		row.Token = secret
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
		if !Ready(row, time.Now()) {
			return ErrUnavailable
		}
		if subtle.ConstantTimeCompare(row.TokenHash, Hash(secret)) != 1 {
			return ErrUnavailable
		}
		if err := s.Repo.Share.PutSession(ctx, &models.ShareSession{TokenHash: Hash(sessionToken), ShareID: id, Version: row.Version, ExpiresAt: row.ExpiresAt}); err != nil {
			return err
		}
		return s.Repo.Share.RecordAccess(ctx, id, time.Now().Unix())
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
		if session == nil || session.ShareID != id || session.ExpiresAt <= time.Now().Unix() {
			return ErrUnavailable
		}
		row, err = s.Repo.Share.Find(ctx, id)
		if err != nil {
			return err
		}
		if !Ready(row, time.Now()) || session.Version != row.Version {
			return ErrUnavailable
		}
		// A response cannot outlive its guest session.
		row.ExpiresAt = min(row.ExpiresAt, session.ExpiresAt)
		return nil
	})
	return row, err
}

// AuthenticateLink is for server-rendered link cards and their media only. It
// does not mint a browser session or record a recipient visit. The normal
// content/API routes still require a guest cookie.
func (s *Service) AuthenticateLink(ctx context.Context, id, secret string) (*models.ShareRecord, error) {
	if !validSecret(secret) {
		return nil, ErrUnavailable
	}
	row, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if !Ready(row, time.Now()) || subtle.ConstantTimeCompare(row.TokenHash, Hash(secret)) != 1 {
		return nil, ErrUnavailable
	}
	return row, nil
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
