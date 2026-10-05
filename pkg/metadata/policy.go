package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/jq"
	"github.com/stashapp/stash/pkg/models"
)

type Source struct {
	CaptureUUID    string `json:"capture_uuid"`
	AttachmentUUID string `json:"attachment_uuid"`
}

// Input is server-derived intake context. HTTP callers choose existing entities
// and files; they cannot claim that an existing entity is newly created.
type Input struct {
	CollectionUUID         string  `json:"collection_uuid"`
	CollectionRevision     int     `json:"collection_revision"`
	PolicyRevision         int     `json:"policy_revision"`
	EntityUUID             string  `json:"entity_uuid"`
	ExpectedEntityRevision int     `json:"expected_entity_revision"`
	RelativePath           string  `json:"relative_path"`
	Created                bool    `json:"created"`
	Source                 *Source `json:"source,omitempty"`
}

type ReferenceCandidate struct {
	UUID           string `json:"uuid"`
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation"`
	Revision       int    `json:"revision"`
}

type NameMatch struct {
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	Candidates []ReferenceCandidate `json:"candidates"`
	More       bool                 `json:"more"`
}

type Change struct {
	Field              string          `json:"field"`
	Status             string          `json:"status"`
	Current            json.RawMessage `json:"current"`
	Value              json.RawMessage `json:"value,omitempty"`
	Origin             string          `json:"origin,omitempty"`
	CaptureUUID        string          `json:"capture_uuid,omitempty"`
	ReferenceRevisions map[string]int  `json:"reference_revisions,omitempty"`
	Names              []NameMatch     `json:"names,omitempty"`
	Message            string          `json:"message,omitempty"`
}

type Preview struct {
	Input   Input                  `json:"context"`
	State   string                 `json:"state"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Changes []Change               `json:"changes"`
	Digest  string                 `json:"digest"`
}

type Service struct{ Repo models.Repository }

// Preview must run in a read transaction. It does not create decisions, source
// evidence, entities, or missing name matches.
func (s Service) Preview(ctx context.Context, input Input) (*Preview, error) {
	return s.preview(ctx, input, true, nil)
}

func (s Service) preview(ctx context.Context, input Input, inspectInactive bool, draft *models.MetadataPolicyDefinition) (*Preview, error) {
	if !archive.ValidRootRelativePath(input.RelativePath, false) || input.CollectionRevision <= 0 || input.PolicyRevision < 0 || input.ExpectedEntityRevision < 0 {
		return nil, errors.New("invalid metadata policy context")
	}
	entity, err := s.Repo.ArchiveEntity.Find(ctx, input.EntityUUID)
	if err != nil {
		return nil, err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || (entity.Kind != models.ArchiveScene && entity.Kind != models.ArchiveImage) ||
		(input.ExpectedEntityRevision != 0 && input.ExpectedEntityRevision != entity.Revision) {
		return nil, models.ErrMetadataFieldConflict
	}
	input.ExpectedEntityRevision = entity.Revision
	ret := &Preview{Input: input, State: "no_policy", Changes: []Change{}}
	policy, err := s.Repo.MetadataPolicy.Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if draft != nil {
		if (policy == nil && input.PolicyRevision != 0) || (policy != nil && policy.Revision != input.PolicyRevision) {
			return nil, models.ErrMetadataPolicyConflict
		}
		// An unsaved definition is bound to the reviewed collection revision.
		// Never publish it, or lend it the saved policy's Apply digest.
		policy = &models.MetadataPolicy{MetadataPolicyRef: models.MetadataPolicyRef{CollectionUUID: input.CollectionUUID, Revision: input.PolicyRevision},
			CollectionRevision: input.CollectionRevision, Definition: *draft}
	}
	if policy == nil {
		if input.PolicyRevision != 0 {
			ret.State = "policy_changed"
		}
		return finishPreview(ret)
	}
	if policy.Revision != input.PolicyRevision {
		ret.State = "policy_changed"
		return finishPreview(ret)
	}
	collection, err := s.Repo.SourceCollection.Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State != "active" || collection.Revision != input.CollectionRevision || policy.CollectionRevision != collection.Revision ||
		collection.RootUUID == nil || (collection.PathPrefix != "." && !strings.HasPrefix(input.RelativePath, collection.PathPrefix+"/")) {
		ret.State = "collection_changed"
		return finishPreview(ret)
	}
	blocked := ""
	if !policy.Definition.Enabled {
		blocked = "disabled"
	}
	rule, found := policy.Definition.Rules[entity.Kind]
	if !found || (input.Created && !rule.OnCreate) || (!input.Created && !rule.OnExisting) {
		blocked = "not_enabled_for_event"
	}
	if blocked != "" && (!inspectInactive || !found) {
		ret.State = blocked
		return finishPreview(ret)
	}
	states := make(map[string]*models.MetadataFieldState)
	values := map[string]interface{}{"uuid": entity.UUID, "kind": entity.Kind}
	for _, field := range models.MetadataFields(entity.Kind) {
		state, err := s.Repo.MetadataField.State(ctx, entity.UUID, field.Name)
		if err != nil {
			return nil, err
		}
		states[field.Name], values[field.Name] = state, state.Value
	}
	if input.Created && rule.SkipOrganizedOnCreate && bytes.Equal(states["organized"].Value, []byte("true")) {
		blocked = "organized_at_creation"
		if !inspectInactive {
			ret.State = blocked
			return finishPreview(ret)
		}
	}
	capture, err := s.capture(ctx, input, entity)
	if err != nil {
		return nil, err
	}
	var source interface{}
	if capture != nil {
		payload, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return nil, err
		}
		source = map[string]interface{}{"post_uuid": capture.PostUUID, "capture_uuid": capture.UUID, "metadata": capture.Metadata, "payload": payload}
	}
	ret.State = "ready"
	if blocked != "" {
		ret.State = blocked
	}
	ret.Data = map[string]interface{}{
		"source": source, "entity": values,
		"context": map[string]interface{}{"created": input.Created, "filename": path.Base(input.RelativePath), "relative_path": input.RelativePath},
	}
	expressions := make(map[string]interface{})
	keys := make([]string, 0, len(rule.Mappings))
	for field, mapping := range rule.Mappings {
		keys = append(keys, field)
		if mapping.JQ != "" {
			expressions[field] = mapping.JQ
		}
	}
	sort.Strings(keys)
	mapped, mappingErr := jq.EvaluateMappingsWithInputLimit(ctx, expressions, ret.Data, 12<<20)
	for _, field := range keys {
		mapping, state := rule.Mappings[field], states[field]
		change := Change{Field: field, Current: state.Value, Status: "protected"}
		value, origin := mapping.Value, "policy"
		if mapping.JQ != "" {
			if mappingErr != nil {
				change.Status, change.Message = "review", mappingErr.Error()
				if state.Protected {
					change.Status = "protected"
				}
				ret.Changes = append(ret.Changes, change)
				continue
			}
			output, exists := mapped[field]
			if !exists {
				change.Status = "omitted"
				if state.Protected {
					change.Status = "protected"
				}
				ret.Changes = append(ret.Changes, change)
				continue
			}
			value, err = json.Marshal(output)
			if err != nil {
				return nil, err
			}
			if capture != nil {
				origin, change.CaptureUUID = "source", capture.UUID
			}
		}
		change.Origin = origin
		change.Value, change.ReferenceRevisions, change.Names, err = s.normalize(ctx, entity.Kind, field, value, mapping.UsesNames())
		if err == nil && mapping.UsesNames() {
			unresolved := false
			for _, name := range change.Names {
				unresolved = unresolved || name.Status != "matched"
			}
			if unresolved {
				var merged json.RawMessage
				merged, err = preserveUnresolvedReferences(field, state.Value, change.Value)
				if err == nil {
					change.Value, change.ReferenceRevisions, _, err = s.normalize(ctx, entity.Kind, field, merged, false)
				}
			}
		}
		if err != nil {
			change.Status, change.Message = "review", err.Error()
		} else {
			change.Status, change.Message, err = s.precedence(ctx, input, state, change, capture)
			if err != nil {
				return nil, err
			}
		}
		if state.Protected {
			change.Status = "protected"
		}
		ret.Changes = append(ret.Changes, change)
	}
	if rule.FilenameTitleFallback {
		fallback, omittedIndex := true, -1
		for index, change := range ret.Changes {
			if change.Field == "title" && change.Status == "omitted" {
				omittedIndex = index
			}
			if change.Field == "title" && change.Status != "omitted" {
				fallback = false
			}
		}
		state := states["title"]
		if fallback && !state.Protected && bytes.Equal(state.Value, []byte(`""`)) {
			filename := path.Base(input.RelativePath)
			title := strings.TrimSuffix(filename, path.Ext(filename))
			value, err := json.Marshal(title)
			if err != nil {
				return nil, err
			}
			status := "ready"
			if bytes.Equal(state.Value, value) {
				status = "unchanged"
			}
			change := Change{Field: "title", Current: state.Value, Value: value, Origin: "filename", Status: status}
			if omittedIndex >= 0 {
				ret.Changes[omittedIndex] = change
			} else {
				ret.Changes = append(ret.Changes, change)
			}
		}
	}
	if rule.MarkOrganized && !states["organized"].Protected {
		selected, unresolved := false, false
		for _, change := range ret.Changes {
			if change.Status == "review" || len(change.Names) != 0 {
				for _, name := range change.Names {
					unresolved = unresolved || name.Status != "matched"
				}
				unresolved = unresolved || change.Status == "review"
			}
			selected = selected || (change.Origin != "filename" && (change.Status == "ready" || change.Status == "unchanged"))
		}
		if selected && !unresolved && !bytes.Equal(states["organized"].Value, []byte("true")) {
			missing := missingOrganizedFields(rule.OrganizedRequires, states, ret.Changes)
			change := Change{Field: "organized", Current: states["organized"].Value, Origin: "policy", Status: "omitted"}
			if len(missing) == 0 {
				change.Status, change.Value = "ready", json.RawMessage("true")
			} else {
				change.Message = "Required metadata is missing: " + strings.Join(missing, ", ")
			}
			ret.Changes = append(ret.Changes, change)
		}
	}
	return finishPreview(ret)
}

func finishPreview(ret *Preview) (*Preview, error) {
	data, err := json.Marshal(struct {
		Input   Input
		State   string
		Changes []Change
	}{ret.Input, ret.State, ret.Changes})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	ret.Digest = hex.EncodeToString(hash[:])
	return ret, nil
}

func (s Service) capture(ctx context.Context, input Input, entity *models.ArchiveEntity) (*models.SourceCapture, error) {
	if input.Source == nil {
		return nil, nil
	}
	source := input.Source
	present, err := s.Repo.SourceCollection.HasCapture(ctx, models.CollectionCapture{CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, CaptureUUID: source.CaptureUUID})
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, models.ErrMetadataPolicyConflict
	}
	present, err = s.Repo.SourceAttachment.InCapture(ctx, source.CaptureUUID, source.AttachmentUUID)
	if err != nil {
		return nil, err
	}
	choice, err := s.Repo.SourceAttachment.MediaDecision(ctx, source.AttachmentUUID)
	if err != nil {
		return nil, err
	}
	if !present || choice == nil || choice.State != "linked" || choice.MediaUUID == nil {
		return nil, models.ErrMetadataPolicyConflict
	}
	chosen, err := s.Repo.ArchiveEntity.Resolve(ctx, *choice.MediaUUID)
	if err != nil {
		return nil, err
	}
	if chosen == nil || chosen.UUID != entity.UUID || chosen.State != models.ArchiveEntityActive {
		return nil, models.ErrMetadataPolicyConflict
	}
	capture, err := s.Repo.SourceEvidence.FindCapture(ctx, source.CaptureUUID)
	if err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, models.ErrMetadataPolicyConflict
	}
	post, err := s.Repo.SourceEvidence.FindPost(ctx, capture.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.State != "active" {
		return nil, models.ErrMetadataPolicyConflict
	}
	return capture, nil
}

func (s Service) precedence(ctx context.Context, input Input, current *models.MetadataFieldState, change Change, capture *models.SourceCapture) (string, string, error) {
	if bytes.Equal(current.Value, change.Value) {
		return "unchanged", "", nil
	}
	if current.Decision != nil {
		if policy := current.Decision.Policy; policy != nil && policy.CollectionUUID != input.CollectionUUID && current.Origin != "filename" {
			return "review", "A different collection selected this field", nil
		}
		if old := current.Decision.CaptureUUID; old != nil {
			if capture == nil {
				return "review", "A source capture already selected this field", nil
			}
			previous, err := s.Repo.SourceEvidence.FindCapture(ctx, *old)
			if err != nil {
				return "", "", err
			}
			if previous == nil || previous.PostUUID != capture.PostUUID {
				return "review", "Another source post already selected this field", nil
			}
			if previous.UUID != capture.UUID && !capture.CapturedAt.After(previous.CapturedAt) {
				return "older_capture", "An equal or newer capture already selected this field", nil
			}
		}
	}
	return "ready", "", nil
}

// Apply evaluates again in the caller's write transaction. A reviewed digest
// includes current field choices and name candidates, so a new collision makes
// that preview stale. Background intake uses an empty digest and pinned policy.
func (s Service) Apply(ctx context.Context, input Input, expectedDigest string) (*Preview, error) {
	preview, err := s.preview(ctx, input, false, nil)
	if err != nil {
		return nil, err
	}
	if expectedDigest != "" && preview.Digest != expectedDigest {
		return nil, models.ErrMetadataPolicyConflict
	}
	for i := range preview.Changes {
		change := &preview.Changes[i]
		if change.Status != "ready" {
			continue
		}
		current, err := s.Repo.MetadataField.State(ctx, input.EntityUUID, change.Field)
		if err != nil {
			return nil, err
		}
		_, err = s.Repo.MetadataField.ApplyAutomatic(ctx, models.MetadataFieldDecisionInput{
			EntityUUID: input.EntityUUID, ExpectedEntityRevision: current.Entity.Revision, Field: change.Field, Mode: "inherit",
			Value: change.Value, Origin: change.Origin, CaptureUUID: change.CaptureUUID, ReferenceRevisions: change.ReferenceRevisions,
			Policy: &models.MetadataPolicyRef{CollectionUUID: input.CollectionUUID, Revision: input.PolicyRevision}, Reason: "Collection metadata policy",
		})
		if err != nil {
			return nil, err
		}
		change.Status = "applied"
	}
	return preview, nil
}

func (p *Preview) ReviewFields() []string {
	ret := []string{}
	if p.State == "policy_changed" || p.State == "collection_changed" || p.State == "ambiguous_directory" {
		ret = append(ret, p.State)
	}
	for _, change := range p.Changes {
		if change.Status == "protected" {
			continue
		}
		review := change.Status == "review"
		for _, name := range change.Names {
			review = review || name.Status != "matched"
		}
		if review {
			ret = append(ret, change.Field)
		}
	}
	return ret
}

func (p *Preview) AppliedFields() []string {
	ret := []string{}
	for _, change := range p.Changes {
		if change.Status == "applied" {
			ret = append(ret, change.Field)
		}
	}
	return ret
}

func unresolvedNames() error {
	return errors.New("no unambiguous name matches; choose explicit native UUIDs")
}
