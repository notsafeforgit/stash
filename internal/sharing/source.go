package sharing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
)

const PageSize = 50

func Source(row *models.ShareRecord) (*models.ShareSource, error) {
	if len(row.SourceJSON) == 0 {
		return nil, nil
	}
	var source models.ShareSource
	if err := json.Unmarshal(row.SourceJSON, &source); err != nil {
		return nil, err
	}
	if source.Mode == "" {
		return nil, nil
	}
	if source.Mode != "SNAPSHOT" && source.Mode != "ONGOING" {
		return nil, errors.New("invalid share mode")
	}
	return &source, nil
}

func (s *Service) validateSource(ctx context.Context, source *models.ShareSource) error {
	if source.Mode != "SNAPSHOT" && source.Mode != "ONGOING" {
		return errors.New("choose snapshot or ongoing membership")
	}
	if (source.Filter == nil) == (source.Entity == nil) {
		return errors.New("choose exactly one filter or entity source")
	}
	if source.Filter != nil {
		if len(source.MediaTypes) != 0 {
			return errors.New("a filtered list has one media type")
		}
		if source.Filter.AST != nil {
			if err := source.Filter.AST.Validate(); err != nil {
				return err
			}
		}
	} else {
		if len(source.MediaTypes) == 0 || len(source.MediaTypes) > 3 {
			return errors.New("choose at least one media type")
		}
		seen := map[string]bool{}
		for _, kind := range source.MediaTypes {
			if !slices.Contains([]string{"SCENE", "IMAGE", "GALLERY"}, kind) || seen[kind] {
				return errors.New("invalid or duplicate media type")
			}
			seen[kind] = true
		}
		ctx = entityid.WithRepository(ctx, s.Repo)
		id, err := entityid.Local(ctx, models.ArchiveEntityKind(strings.ToLower(source.Entity.Kind)), source.Entity.UUID)
		if err != nil {
			return err
		}
		switch source.Entity.Kind {
		case "PERFORMER":
			v, err := s.Repo.Performer.Find(ctx, id)
			if err != nil {
				return err
			}
			if v == nil {
				return ErrUnavailable
			}
			source.Entity.Name = v.Name
		case "STUDIO":
			v, err := s.Repo.Studio.Find(ctx, id)
			if err != nil {
				return err
			}
			if v == nil {
				return ErrUnavailable
			}
			source.Entity.Name = v.Name
		case "TAG":
			v, err := s.Repo.Tag.Find(ctx, id)
			if err != nil {
				return err
			}
			if v == nil {
				return ErrUnavailable
			}
			source.Entity.Name = v.Name
		default:
			return errors.New("unsupported share source")
		}
	}
	// Compile and execute every query before storing any grant, including empty
	// ongoing sources. Invalid criteria must never turn into an unfiltered share.
	queries, err := s.sourceQueries(ctx, source)
	if err != nil {
		return err
	}
	for _, query := range queries {
		if _, err := s.Repo.Share.QueryTargets(ctx, query, 0, 1, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) sourceQueries(ctx context.Context, source *models.ShareSource) ([]models.ShareFilter, error) {
	ctx = entityid.WithRepository(ctx, s.Repo)
	var queries []models.ShareFilter
	if source.Filter != nil {
		queries = append(queries, *source.Filter)
	} else if source.Entity != nil {
		field := map[string]string{"PERFORMER": "performers", "STUDIO": "studios", "TAG": "tags"}[source.Entity.Kind]
		if field == "" {
			return nil, errors.New("unsupported share source")
		}
		for _, kind := range source.MediaTypes {
			value := map[string]any{"value": []any{source.Entity.UUID}, "modifier": "INCLUDES"}
			if source.Entity.Kind != "PERFORMER" {
				value["depth"] = 0
			}
			queries = append(queries, models.ShareFilter{Kind: kind, AST: &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: field, Value: value}}}})
		}
	} else {
		return nil, errors.New("missing share source")
	}
	for i := range queries {
		var err error
		queries[i].AST, err = resolveShareFilter(ctx, strings.ToLower(queries[i].Kind), queries[i].AST)
		if err != nil {
			return nil, err
		}
	}
	return queries, nil
}

// Resolve a copy: saved criteria always retain UUIDs. Missing UUIDs remain
// nonmatching, including negative/exclusion criteria, just like library queries.
func resolveShareFilter(ctx context.Context, kind string, ast *models.FilterAST) (*models.FilterAST, error) {
	if ast == nil {
		return nil, nil
	}
	data, err := json.Marshal(ast)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	tree, err = entityid.MapFilterAST(kind, tree, func(kind string, raw any) (any, error) {
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("share filters must use library UUIDs")
		}
		id, err := entityid.FilterKey(ctx, models.ArchiveEntityKind(kind), value)
		return strconv.Itoa(id), err
	})
	if err != nil {
		return nil, err
	}
	data, err = json.Marshal(tree)
	if err != nil {
		return nil, err
	}
	var ret models.FilterAST
	return &ret, json.Unmarshal(data, &ret)
}

type sourceCursor struct {
	After []int `json:"after"`
}

func (s *Service) sourceTargets(ctx context.Context, source *models.ShareSource, cursor string, limit int) ([]Target, string, error) {
	queries, err := s.sourceQueries(ctx, source)
	if err != nil {
		return nil, "", err
	}
	position := sourceCursor{After: make([]int, len(queries))}
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(data) > 100 {
			return nil, "", errors.New("invalid share cursor")
		}
		if err := json.Unmarshal(data, &position); err != nil {
			return nil, "", err
		}
		if len(position.After) != len(queries) {
			return nil, "", errors.New("invalid share cursor")
		}
		for _, after := range position.After {
			if after < -1 {
				return nil, "", errors.New("invalid share cursor")
			}
		}
	}
	ret := []Target{}
	more := false
	for index, query := range queries {
		if position.After[index] == -1 {
			continue
		}
		ids, err := s.Repo.Share.QueryTargets(ctx, query, position.After[index], limit, nil)
		if err != nil {
			return nil, "", err
		}
		for _, id := range ids {
			ret = append(ret, Target{Kind: query.Kind, ID: id})
			position.After[index] = id
		}
		if len(ids) == limit {
			more = true
		} else {
			position.After[index] = -1
		}
	}
	if more {
		data, err := json.Marshal(position)
		return ret, base64.RawURLEncoding.EncodeToString(data), err
	}
	return ret, "", nil
}

// Content reevaluates ongoing membership in one read transaction. A cursor only
// selects the next bounded query page; it never widens the saved grant.
func (s *Service) Content(ctx context.Context, row *models.ShareRecord, cursor string, focus ...string) (models.ShareSnapshot, string, error) {
	source, err := Source(row)
	if err != nil {
		return models.ShareSnapshot{}, "", err
	}
	var ret models.ShareSnapshot
	var next string
	err = s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if len(focus) > 2 {
			return errors.New("too many focused share items")
		}
		if source == nil || source.Mode == "SNAPSHOT" {
			if cursor != "" {
				return errors.New("snapshot has no continuation")
			}
			ret, err = s.visibleSnapshot(ctx, row)
			return err
		}
		targets, continuation, err := s.sourceTargets(ctx, source, cursor, PageSize)
		if err != nil {
			return err
		}
		next = continuation
		// Detail links must work before their browse page is loaded. Focus only
		// adds individually authorized targets and does not affect the cursor.
		for _, key := range focus {
			if key == "" {
				continue
			}
			target, err := s.targetFromKey(ctx, key)
			if errors.Is(err, ErrUnavailable) {
				continue
			}
			if err != nil {
				return err
			}
			matched, err := s.matches(ctx, source, target)
			if err != nil {
				return err
			}
			if matched {
				targets = append(targets, target)
			}
		}
		snapshot, err := s.buildSnapshot(ctx, targets, true)
		if err != nil {
			return err
		}
		ret = *snapshot
		return nil
	})
	return ret, next, err
}

func (s *Service) matches(ctx context.Context, source *models.ShareSource, target Target) (bool, error) {
	queries, err := s.sourceQueries(ctx, source)
	if err != nil {
		return false, err
	}
	for _, query := range queries {
		if query.Kind == target.Kind {
			ids, err := s.Repo.Share.QueryTargets(ctx, query, 0, 1, &target.ID)
			if err != nil {
				return false, err
			}
			if len(ids) != 0 {
				return true, nil
			}
		}
		if query.Kind == "GALLERY" && target.Kind != "GALLERY" {
			var galleries []*models.Gallery
			var err error
			if target.Kind == "IMAGE" {
				galleries, err = s.Repo.Gallery.FindByImageID(ctx, target.ID)
			} else {
				galleries, err = s.Repo.Gallery.FindBySceneID(ctx, target.ID)
			}
			if err != nil {
				return false, err
			}
			for _, gallery := range galleries {
				ids, err := s.Repo.Share.QueryTargets(ctx, query, 0, 1, &gallery.ID)
				if err != nil {
					return false, err
				}
				if len(ids) != 0 {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func (s *Service) targetFromKey(ctx context.Context, key string) (Target, error) {
	kind, uuid, found := strings.Cut(key, "-")
	if !found || !slices.Contains([]string{"scene", "image", "gallery"}, kind) {
		return Target{}, ErrUnavailable
	}
	identity, err := s.Repo.ArchiveEntity.Find(ctx, uuid)
	if err != nil {
		return Target{}, err
	}
	if identity == nil || identity.Kind != models.ArchiveEntityKind(kind) || identity.State != models.ArchiveEntityActive || identity.LocalID == nil {
		return Target{}, ErrUnavailable
	}
	return Target{Kind: strings.ToUpper(kind), ID: *identity.LocalID}, nil
}

// Item is the common authorization path for original files and every derivative.
// Dynamic keys contain UUIDs, so deleting/recreating an entity never reuses a URL.
func (s *Service) Item(ctx context.Context, row *models.ShareRecord, key string) (*models.ShareMedia, models.File, *models.Scene, error) {
	source, err := Source(row)
	if err != nil {
		return nil, nil, nil, err
	}
	var item *models.ShareMedia
	err = s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if source == nil || source.Mode == "SNAPSHOT" {
			snapshot, err := Snapshot(row)
			if err != nil {
				return err
			}
			// Check the original entry too: deleting a shared gallery must not
			// keep its otherwise-unselected images accessible through old URLs.
			allowed := false
			for _, entry := range snapshot.Entries {
				if slices.Contains(entry.MediaKeys, key) {
					active, err := s.entryActive(ctx, entry)
					if err != nil {
						return err
					}
					if active {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				return ErrUnavailable
			}
			for _, media := range snapshot.Media {
				if media.Key == key {
					item = &media
					return nil
				}
			}
			return ErrUnavailable
		}
		target, err := s.targetFromKey(ctx, key)
		if err != nil {
			return err
		}
		if target.Kind == "GALLERY" {
			return ErrUnavailable
		}
		matched, err := s.matches(ctx, source, target)
		if err != nil {
			return err
		}
		if !matched {
			return ErrUnavailable
		}
		snapshot, err := s.buildSnapshot(ctx, []Target{target}, true)
		if err != nil {
			return err
		}
		if len(snapshot.Media) != 1 {
			return ErrUnavailable
		}
		item = &snapshot.Media[0]
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	f, scene, err := s.Resolve(ctx, *item)
	return item, f, scene, err
}

func (s *Service) entryActive(ctx context.Context, entry models.ShareEntry) (bool, error) {
	identity, err := s.Repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveEntityKind(strings.ToLower(entry.Kind)), entry.ID)
	if err != nil {
		return false, err
	}
	return identity != nil && identity.State == models.ArchiveEntityActive && (entry.UUID == "" || entry.UUID == identity.UUID), nil
}

func (s *Service) visibleSnapshot(ctx context.Context, row *models.ShareRecord) (models.ShareSnapshot, error) {
	saved, err := Snapshot(row)
	if err != nil {
		return saved, err
	}
	ret := models.ShareSnapshot{Entries: []models.ShareEntry{}, Media: []models.ShareMedia{}}
	allowed := map[string]bool{}
	for index, entry := range saved.Entries {
		active, err := s.entryActive(ctx, entry)
		if err != nil {
			return ret, err
		}
		if !active {
			continue
		}
		if entry.Key == "" {
			entry.Key = fmt.Sprintf("gallery-%d", index+1)
		}
		ret.Entries = append(ret.Entries, entry)
		for _, key := range entry.MediaKeys {
			allowed[key] = true
		}
	}
	visible := map[string]bool{}
	for _, media := range saved.Media {
		if !allowed[media.Key] {
			continue
		}
		_, _, err := s.resolve(ctx, media)
		if errors.Is(err, ErrUnavailable) {
			continue
		}
		if err != nil {
			return ret, err
		}
		visible[media.Key] = true
		ret.Media = append(ret.Media, media)
	}
	entries := ret.Entries[:0]
	for _, entry := range ret.Entries {
		entry.MediaKeys = slices.DeleteFunc(entry.MediaKeys, func(key string) bool { return !visible[key] })
		if len(entry.MediaKeys) > 0 {
			entries = append(entries, entry)
		}
	}
	ret.Entries = entries
	return ret, nil
}
