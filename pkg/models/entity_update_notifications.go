package models

import (
	"context"
	"errors"
	"slices"

	"github.com/stashapp/stash/pkg/txn"
)

// EntityUpdateNotifier observes committed domain edits. It cannot veto them.
type EntityUpdateNotifier func(context.Context, ArchiveEntityKind, int, []string)

type entityUpdateNotifierKey struct{}

type notifyingTxnManager struct {
	TxnManager
	notify EntityUpdateNotifier
}

// WithEntityUpdateNotifier carries the application's observer through shared
// transactions without making repositories depend on the plugin runtime.
func WithEntityUpdateNotifier(manager TxnManager, notify EntityUpdateNotifier) TxnManager {
	return notifyingTxnManager{TxnManager: manager, notify: notify}
}

func (m notifyingTxnManager) Begin(ctx context.Context, writable bool) (context.Context, error) {
	ctx, err := m.TxnManager.Begin(ctx, writable)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, entityUpdateNotifierKey{}, m.notify), nil
}

// NotifyEntityUpdate queues an update after commit using the caller's original
// context. Failed transactions and offline repositories have no notifications.
func NotifyEntityUpdate(ctx context.Context, kind ArchiveEntityKind, id int, fields []string) error {
	notify, _ := ctx.Value(entityUpdateNotifierKey{}).(EntityUpdateNotifier)
	if notify == nil || len(fields) == 0 {
		return nil
	}
	if !txn.HasHooks(ctx) {
		return errors.New("entity update notification requires a managed transaction")
	}
	fields = slices.Clone(fields)
	txn.AddPostCommitHook(ctx, func(ctx context.Context) { notify(ctx, kind, id, fields) })
	return nil
}
