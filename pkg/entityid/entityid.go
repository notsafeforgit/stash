// Package entityid implements the boundary between public library UUIDs and
// repository keys. Local keys never identify an entity across installations,
// deletion/recreation, or a merge.
package entityid

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type contextKey struct{}

type Resolver struct {
	repo    models.Repository
	cache   bool
	mu      sync.Mutex
	public  map[string]string
	missing map[string]int
}

func WithRepository(ctx context.Context, repo models.Repository) context.Context {
	return WithResolver(ctx, NewResolver(repo, false))
}

func NewResolver(repo models.Repository, cache bool) *Resolver {
	return &Resolver{repo: repo, cache: cache, public: make(map[string]string), missing: make(map[string]int)}
}
func WithResolver(ctx context.Context, resolver *Resolver) context.Context {
	return context.WithValue(ctx, contextKey{}, resolver)
}

// Remember retains an identity captured before a delete or merge. It is only
// used for committed notifications; ordinary reads still require an active row.
func Remember(ctx context.Context, kind models.ArchiveEntityKind, id int, value string) {
	if r, err := From(ctx); err == nil {
		r.remember(kind, id, value)
	}
}

func Recorded(ctx context.Context, kind models.ArchiveEntityKind, id int) string {
	r, err := From(ctx)
	if err != nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.public[string(kind)+":"+strconv.Itoa(id)]
}

type identityTxnManager struct {
	models.TxnManager
	repo models.Repository
}

func WithTransactions(repo models.Repository) models.Repository {
	repo.TxnManager = identityTxnManager{TxnManager: repo.TxnManager, repo: repo}
	return repo
}

func (m identityTxnManager) Begin(ctx context.Context, writable bool) (context.Context, error) {
	if _, err := From(ctx); err != nil {
		ctx = WithRepository(ctx, m.repo)
	}
	return m.TxnManager.Begin(ctx, writable)
}

func From(ctx context.Context) (*Resolver, error) {
	r, _ := ctx.Value(contextKey{}).(*Resolver)
	if r == nil || r.repo.ArchiveEntity == nil {
		return nil, errors.New("library identity resolver unavailable")
	}
	return r, nil
}

// Local resolves the current entity, including merge redirects. Retired UUIDs
// cannot be resolved through original_id, even if SQLite reuses that key.
func Local(ctx context.Context, kind models.ArchiveEntityKind, value string) (int, error) {
	return local(ctx, kind, value, false)
}

// FilterKey keeps a deleted or unknown UUID as a nonmatching condition.
func FilterKey(ctx context.Context, kind models.ArchiveEntityKind, value string) (int, error) {
	return local(ctx, kind, value, true)
}

func local(ctx context.Context, kind models.ArchiveEntityKind, value string, filter bool) (int, error) {
	u, err := uuid.Parse(value)
	if err != nil || u == uuid.Nil || u.String() != value {
		return 0, errors.New("library ID must be a canonical UUID")
	}
	r, err := From(ctx)
	if err != nil {
		return 0, err
	}
	var entity *models.ArchiveEntity
	err = r.read(ctx, func(ctx context.Context) error {
		var err error
		entity, err = r.repo.ArchiveEntity.Resolve(ctx, value)
		return err
	})
	if err != nil {
		return 0, err
	}
	if filter && (entity == nil || ((kind == "" || entity.Kind == kind) && entity.State == models.ArchiveEntityDeleted)) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if id, ok := r.missing[value]; ok {
			return id, nil
		}
		id := -len(r.missing) - 1
		r.missing[value] = id
		return id, nil
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.LocalID == nil || (kind != "" && entity.Kind != kind) {
		return 0, errors.New("library identity is unavailable or has the wrong kind")
	}
	r.remember(entity.Kind, *entity.LocalID, entity.UUID)
	return *entity.LocalID, nil
}

func (r *Resolver) remember(kind models.ArchiveEntityKind, id int, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.public[string(kind)+":"+strconv.Itoa(id)] = value
}

func Public(ctx context.Context, kind models.ArchiveEntityKind, id int) (string, error) {
	r, err := From(ctx)
	if err != nil {
		return "", err
	}
	key := string(kind) + ":" + strconv.Itoa(id)
	r.mu.Lock()
	value := r.public[key]
	r.mu.Unlock()
	if r.cache && value != "" {
		return value, nil
	}
	var entity *models.ArchiveEntity
	err = r.read(ctx, func(ctx context.Context) error {
		var err error
		entity, err = r.repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
		return err
	})
	if err != nil {
		return "", err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive {
		return "", fmt.Errorf("%s identity unavailable", kind)
	}
	r.remember(kind, id, entity.UUID)
	return entity.UUID, nil
}

// Identity capture inside a managed transaction must use that same snapshot.
// Starting a nested transaction would fail (or read a different generation).
func (r *Resolver) read(ctx context.Context, fn txn.TxnFunc) error {
	if txn.HasHooks(ctx) {
		return fn(ctx)
	}
	return r.repo.WithReadTxn(ctx, fn)
}
