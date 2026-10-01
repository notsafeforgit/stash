package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/stashapp/stash/pkg/models"
)

func (s Service) matchNames(ctx context.Context, raw json.RawMessage) (json.RawMessage, []NameMatch, error) {
	names, err := PerformerNames(raw)
	if err != nil {
		return nil, nil, err
	}
	ids, matches := []string{}, []NameMatch{}
	for _, name := range names {
		candidates, err := s.Repo.Performer.FindByNameOrAlias(ctx, name, 101)
		if err != nil {
			return nil, nil, err
		}
		match := NameMatch{Name: name, Status: "unmatched", Candidates: []PerformerCandidate{}, More: len(candidates) > 100}
		if len(candidates) > 100 {
			candidates = candidates[:100]
		}
		for _, candidate := range candidates {
			identity, err := s.Repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, candidate.ID)
			if err != nil {
				return nil, nil, err
			}
			if identity == nil || identity.State != models.ArchiveEntityActive {
				return nil, nil, models.ErrMetadataFieldConflict
			}
			match.Candidates = append(match.Candidates, PerformerCandidate{UUID: identity.UUID, Name: candidate.Name, Disambiguation: candidate.Disambiguation, Revision: identity.Revision})
		}
		sort.Slice(match.Candidates, func(i, j int) bool { return match.Candidates[i].UUID < match.Candidates[j].UUID })
		switch {
		case match.More || len(match.Candidates) > 1:
			match.Status = "ambiguous"
		case len(match.Candidates) == 1:
			match.Status = "matched"
			ids = append(ids, match.Candidates[0].UUID)
		}
		matches = append(matches, match)
	}
	if len(names) != 0 && len(ids) == 0 {
		return nil, matches, unresolvedNames()
	}
	value, err := json.Marshal(ids)
	return value, matches, err
}

func (s Service) normalize(ctx context.Context, kind models.ArchiveEntityKind, field string, value json.RawMessage, names bool) (json.RawMessage, map[string]int, []NameMatch, error) {
	var matches []NameMatch
	var err error
	if names {
		value, matches, err = s.matchNames(ctx, value)
		if err != nil {
			return nil, nil, matches, err
		}
	}
	var definition models.MetadataFieldDefinition
	for _, candidate := range models.MetadataFields(kind) {
		if candidate.Name == field {
			definition = candidate
		}
	}
	revisions := make(map[string]int)
	if definition.ReferenceKind != "" {
		value, revisions, err = s.resolveReferences(ctx, definition, value)
		if err != nil {
			return nil, nil, matches, err
		}
	}
	normalized, err := s.Repo.MetadataField.Normalize(ctx, kind, field, value, revisions)
	return normalized, revisions, matches, err
}

func (s Service) resolveReferences(ctx context.Context, field models.MetadataFieldDefinition, raw json.RawMessage) (json.RawMessage, map[string]int, error) {
	type groupReference struct {
		UUID       string `json:"uuid"`
		SceneIndex *int   `json:"scene_index,omitempty"`
	}
	var ids []string
	var groups []groupReference
	switch field.Type {
	case "reference":
		var id *string
		if err := json.Unmarshal(raw, &id); err != nil {
			return nil, nil, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	case "references":
		if err := json.Unmarshal(raw, &ids); err != nil || ids == nil {
			return nil, nil, errors.New("relationship mapping requires an array of native UUIDs")
		}
	case "groups":
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&groups); err != nil || groups == nil {
			return nil, nil, errors.New("groups requires an array of UUID and scene_index objects")
		}
		for _, group := range groups {
			ids = append(ids, group.UUID)
		}
	}
	if len(ids) > 4096 {
		return nil, nil, errors.New("relationship mapping exceeds 4096 targets")
	}
	revisions := make(map[string]int)
	for i, id := range ids {
		target, err := s.Repo.ArchiveEntity.Resolve(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if target == nil || target.State != models.ArchiveEntityActive || target.Kind != field.ReferenceKind || target.LocalID == nil {
			return nil, nil, errors.New("relationship target is missing, deleted, or a different kind")
		}
		ids[i], revisions[target.UUID] = target.UUID, target.Revision
	}
	var value interface{}
	switch field.Type {
	case "reference":
		if len(ids) > 0 {
			value = ids[0]
		}
	case "references":
		value = ids
	case "groups":
		for i := range groups {
			groups[i].UUID = ids[i]
		}
		value = groups
	}
	encoded, err := json.Marshal(value)
	return encoded, revisions, err
}
