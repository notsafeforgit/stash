// Package ingest owns the transactional boundary shared by native producers.
// A successful response is emitted only after receipt and domain writes commit.
package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
)

var (
	ErrUnauthorized = errors.New("invalid or inactive producer credential")
	ErrForbidden    = errors.New("producer credential does not permit this collection and root")
	ErrInvalid      = errors.New("invalid ingestion event")
	ErrUnsupported  = errors.New("unsupported ingestion protocol, kind, or source identity")
	ErrDefinition   = errors.New("collection definition changed or is inactive")
	ErrNotFound     = errors.New("ingestion record not found")
)

type Service struct{ Repo models.Repository }

func New(repo models.Repository) *Service { return &Service{Repo: repo} }

func ValidUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func Digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }

// The 32 random secret bytes are returned once; database reads and JSON exports
// contain only a SHA-256 verifier. Tokens are accepted only in Authorization.
func (s *Service) IssueCredential(ctx context.Context, producer string, scopes []models.IngestScope, expires *time.Time, roots ...string) (*models.IngestCredential, string, error) {
	if !ValidUUID(producer) || len(scopes)+len(roots) < 1 || len(scopes)+len(roots) > 128 || (expires != nil && (!expires.After(time.Now()) || expires.Year() > 9999)) {
		return nil, "", ErrInvalid
	}
	seen := make(map[string]bool)
	for _, scope := range scopes {
		if !ValidUUID(scope.CollectionUUID) || seen[scope.CollectionUUID] || (scope.RootUUID != nil && !ValidUUID(*scope.RootUUID)) {
			return nil, "", ErrInvalid
		}
		seen[scope.CollectionUUID] = true
	}
	seenRoots := make(map[string]bool)
	for _, root := range roots {
		if !ValidUUID(root) || seenRoots[root] {
			return nil, "", ErrInvalid
		}
		seenRoots[root] = true
	}
	id := uuid.NewString()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, "", err
	}
	token := "ingest_" + id + "." + base64.RawURLEncoding.EncodeToString(secret)
	var result *models.IngestCredential
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		owner, err := s.Repo.Ingest.FindProducer(ctx, producer)
		if err != nil {
			return err
		}
		if owner == nil {
			return ErrNotFound
		}
		result, err = s.Repo.Ingest.IssueCredential(ctx, models.IngestCredential{UUID: id, ProducerUUID: producer, SecretHash: Digest([]byte(token)), Scopes: scopes, RootUUIDs: roots, ExpiresAt: expires})
		return err
	})
	if err != nil {
		return nil, "", err
	}
	return result, token, nil
}

// Call inside the transaction performing a read or write, so revocation cannot
// race between a middleware check and publication of a new receipt.
func (s *Service) authenticate(ctx context.Context, token string) (*models.IngestCredential, error) {
	if len(token) != 87 || !strings.HasPrefix(token, "ingest_") || token[43] != '.' || !ValidUUID(token[7:43]) {
		return nil, ErrUnauthorized
	}
	secret, err := base64.RawURLEncoding.DecodeString(token[44:])
	if err != nil || len(secret) != 32 {
		return nil, ErrUnauthorized
	}
	credential, err := s.Repo.Ingest.FindCredential(ctx, token[7:43])
	if err != nil {
		return nil, err
	}
	if credential == nil || subtle.ConstantTimeCompare([]byte(credential.SecretHash), []byte(Digest([]byte(token)))) != 1 || credential.Revoked ||
		(credential.ExpiresAt != nil && !time.Now().Before(*credential.ExpiresAt)) {
		return nil, ErrUnauthorized
	}
	return credential, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (*models.IngestCredential, error) {
	var ret *models.IngestCredential
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error { var err error; ret, err = s.authenticate(ctx, token); return err })
	return ret, err
}

func permitted(credential *models.IngestCredential, collection string, root *string) bool {
	if root != nil && permittedRoot(credential, *root) {
		return true
	}
	for _, scope := range credential.Scopes {
		if scope.CollectionUUID == collection && reflect.DeepEqual(scope.RootUUID, root) {
			return true
		}
	}
	return false
}

func permittedRoot(credential *models.IngestCredential, root string) bool {
	for _, allowed := range credential.RootUUIDs {
		if allowed == root {
			return true
		}
	}
	return false
}

func (s *Service) Receipt(ctx context.Context, token, event string) (*models.IngestReceipt, error) {
	if !ValidUUID(event) {
		return nil, ErrInvalid
	}
	var receipt *models.IngestReceipt
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		receipt, err = s.Repo.Ingest.FindReceipt(ctx, credential.ProducerUUID, event)
		if err != nil {
			return err
		}
		if receipt == nil {
			return ErrNotFound
		}
		if !permitted(credential, receipt.CollectionUUID, receipt.RootUUID) {
			return ErrForbidden
		}
		return nil
	})
	return receipt, err
}
