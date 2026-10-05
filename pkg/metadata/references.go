package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

func (s Service) matchNames(ctx context.Context, field models.MetadataFieldDefinition, raw json.RawMessage) (json.RawMessage, []NameMatch, error) {
	names, err := referenceNames(field, raw)
	if err != nil {
		return nil, nil, err
	}
	ids, matches := []string{}, []NameMatch{}
	groups := []groupReference{}
	seen := make(map[string]NameMatch)
	for _, ref := range names {
		match, found := seen[ref.Name]
		if !found {
			match, err = s.matchName(ctx, field.ReferenceKind, ref.Name)
			if err != nil {
				return nil, nil, err
			}
			seen[ref.Name] = match
			matches = append(matches, match)
		}
		if match.Status == "matched" {
			ids = append(ids, match.Candidates[0].UUID)
			groups = append(groups, groupReference{UUID: match.Candidates[0].UUID, SceneIndex: ref.SceneIndex})
		}
	}
	if len(names) != 0 && len(ids) == 0 {
		return nil, matches, unresolvedNames()
	}
	var selected any = ids
	switch field.Type {
	case "reference":
		selected = nil
		if len(ids) > 0 {
			selected = ids[0]
		}
	case "groups":
		selected = groups
	}
	value, err := json.Marshal(selected)
	return value, matches, err
}

func (s Service) matchName(ctx context.Context, kind models.ArchiveEntityKind, name string) (NameMatch, error) {
	match := NameMatch{Name: name, Status: "unmatched", Candidates: []ReferenceCandidate{}}
	candidates, err := s.Repo.MetadataField.NameCandidates(ctx, kind, name)
	if err != nil {
		return match, err
	}
	match.More = len(candidates) > 100
	if match.More {
		candidates = candidates[:100]
	}
	for _, candidate := range candidates {
		match.Candidates = append(match.Candidates, ReferenceCandidate{UUID: candidate.UUID, Name: candidate.Name, Disambiguation: candidate.Disambiguation, Revision: candidate.Revision})
	}
	if match.More || len(match.Candidates) > 1 {
		match.Status = "ambiguous"
	} else if len(match.Candidates) == 1 {
		match.Status = "matched"
	}
	return match, nil
}

func (s Service) normalize(ctx context.Context, kind models.ArchiveEntityKind, field string, value json.RawMessage, names bool) (json.RawMessage, map[string]int, []NameMatch, error) {
	var matches []NameMatch
	var err error
	var definition models.MetadataFieldDefinition
	for _, candidate := range models.MetadataFields(kind) {
		if candidate.Name == field {
			definition = candidate
		}
	}
	if names {
		value, matches, err = s.matchNames(ctx, definition, value)
		if err != nil {
			return nil, nil, matches, err
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
