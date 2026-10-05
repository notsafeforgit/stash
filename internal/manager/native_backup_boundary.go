package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

const NativeCheckpointBoundaryFormat = "org.notsafeforgit.stash.filesystem-checkpoint"
const nativeBoundaryLimit = 64 << 10

var ErrNativeCheckpointBoundaryExpired = errors.New("native checkpoint filesystem boundary is no longer accepting confirmation")

type NativeCheckpointBoundaryInput struct {
	TimeoutSeconds int `json:"timeout_seconds"`
}

type NativeCheckpointBoundaryReady struct {
	UUID          string    `json:"uuid"`
	Token         string    `json:"token"`
	RequestSHA256 string    `json:"request_sha256"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type NativeCheckpointBoundaryConfirmation struct {
	Token   string          `json:"token"`
	Details json.RawMessage `json:"details"`
}

// NativeCheckpointBoundaryRecord binds an authorized external coordinator's
// filesystem evidence to the native writer exclusion interval. Providers must
// establish and validate their actual snapshots/producer barriers themselves;
// this receipt cannot certify an arbitrary caller's filesystem claims.
type NativeCheckpointBoundaryRecord struct {
	Format        string          `json:"format"`
	Version       int             `json:"version"`
	UUID          string          `json:"uuid"`
	RequestSHA256 string          `json:"request_sha256"`
	Token         string          `json:"token"`
	ConfirmedAt   time.Time       `json:"confirmed_at"`
	Details       json.RawMessage `json:"details"`
}

type nativeCheckpointBoundaryPending struct {
	ready   NativeCheckpointBoundaryReady
	ctx     context.Context
	record  *NativeCheckpointBoundaryRecord
	confirm chan *NativeCheckpointBoundaryRecord
}

func cloneBoundaryRecord(record *NativeCheckpointBoundaryRecord) *NativeCheckpointBoundaryRecord {
	if record == nil {
		return nil
	}
	result := *record
	result.Details = append(json.RawMessage(nil), record.Details...)
	return &result
}

func (s *Manager) clearNativeCheckpointBoundary(id string) {
	s.nativeBoundaryMu.Lock()
	defer s.nativeBoundaryMu.Unlock()
	if s.nativeBoundary != nil && s.nativeBoundary.ready.UUID == id {
		s.nativeBoundary = nil
	}
}

func (s *Manager) awaitNativeCheckpointBoundary(ctx context.Context, input NativeCheckpointInput, requestHash string, notify func(NativeCheckpointBoundaryReady) error) (*NativeCheckpointBoundaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Now().UTC().Add(time.Duration(input.ExternalBoundary.TimeoutSeconds) * time.Second)
	if outer, ok := ctx.Deadline(); ok && outer.Before(deadline) {
		deadline = outer.UTC()
	}
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	pending := &nativeCheckpointBoundaryPending{ctx: ctx, confirm: make(chan *NativeCheckpointBoundaryRecord, 1),
		ready: NativeCheckpointBoundaryReady{UUID: input.UUID, Token: uuid.NewString(), RequestSHA256: requestHash, ExpiresAt: deadline}}
	s.nativeBoundaryMu.Lock()
	s.nativeBoundary = pending
	s.nativeBoundaryMu.Unlock()
	if err := notify(pending.ready); err != nil {
		return nil, err
	}
	select {
	case record := <-pending.confirm:
		if err := waitCtx.Err(); err != nil {
			return nil, err
		}
		return cloneBoundaryRecord(record), nil
	case <-waitCtx.Done():
		return nil, waitCtx.Err()
	}
}

func boundaryDetails(input NativeCheckpointBoundaryConfirmation) (json.RawMessage, error) {
	token, err := uuid.Parse(input.Token)
	if err != nil || token.String() != input.Token || len(input.Details) == 0 || len(input.Details) > nativeBoundaryLimit || !json.Valid(input.Details) {
		return nil, ErrNativeCheckpointInvalid
	}
	// Preserve large filesystem GUIDs/inode numbers without float conversion.
	decoder := json.NewDecoder(bytes.NewReader(input.Details))
	decoder.UseNumber()
	var object map[string]interface{}
	if err := decoder.Decode(&object); err != nil || len(object) == 0 {
		return nil, ErrNativeCheckpointInvalid
	}
	body, err := json.Marshal(object)
	if err != nil || len(body) > nativeBoundaryLimit {
		return nil, ErrNativeCheckpointInvalid
	}
	return body, nil
}

// ConfirmNativeCheckpointBoundary accepts only the current one-use challenge.
// An identical acknowledgement remains retryable through the database copy and
// afterwards from its sealed component; no second filesystem capture is needed.
func (s *Manager) ConfirmNativeCheckpointBoundary(ctx context.Context, id string, input NativeCheckpointBoundaryConfirmation) (*NativeCheckpointBoundaryRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	details, err := boundaryDetails(input)
	if err != nil {
		return nil, err
	}
	s.nativeBoundaryMu.Lock()
	pending := s.nativeBoundary
	if pending != nil && pending.ready.UUID == id {
		defer s.nativeBoundaryMu.Unlock()
		if pending.ready.Token != input.Token {
			return nil, ErrNativeCheckpointInvalid
		}
		if pending.ctx.Err() != nil {
			return nil, ErrNativeCheckpointBoundaryExpired
		}
		if pending.record != nil {
			if !bytes.Equal(pending.record.Details, details) {
				return nil, ErrNativeCheckpointInvalid
			}
			return cloneBoundaryRecord(pending.record), nil
		}
		if !time.Now().Before(pending.ready.ExpiresAt) {
			return nil, ErrNativeCheckpointBoundaryExpired
		}
		record := &NativeCheckpointBoundaryRecord{Format: NativeCheckpointBoundaryFormat, Version: 1, UUID: id,
			RequestSHA256: pending.ready.RequestSHA256, Token: input.Token, ConfirmedAt: time.Now().UTC(), Details: details}
		pending.record = record
		pending.confirm <- record
		return cloneBoundaryRecord(record), nil
	}
	s.nativeBoundaryMu.Unlock()
	// The callback may have finished before a retry after a lost response. Only
	// the sealed capture can answer then; an incomplete directory is not proof.
	f, component, err := s.OpenNativeCheckpointComponent(id, "filesystem-boundary.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if component.Bytes > nativeBoundaryLimit+4096 {
		return nil, ErrNativeCheckpointInvalid
	}
	body, err := io.ReadAll(io.LimitReader(f, nativeBoundaryLimit+4097))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != component.SHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	var record NativeCheckpointBoundaryRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return nil, ErrNativeCheckpointInvalid
	}
	canonical, err := json.Marshal(record)
	if err != nil || len(body) > nativeBoundaryLimit+4096 || !bytes.Equal(body, canonical) || record.Format != NativeCheckpointBoundaryFormat ||
		record.Version != 1 || record.UUID != id || record.Token != input.Token || record.ConfirmedAt.IsZero() || !bytes.Equal(record.Details, details) {
		return nil, ErrNativeCheckpointInvalid
	}
	manifest, err := s.ReadNativeCheckpoint(id)
	if err != nil {
		return nil, err
	}
	if record.RequestSHA256 != manifest.RequestSHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	return &record, nil
}
