package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Only the previously installed default expressions are rewritten. An arbitrary
// user expression that still reads discarded inputs requires an explicit change.
const oldNFODisplayMapping = `def nonempty: type == "string" and length > 0;
.source as $source
| if $source == null then empty
  else ($source.payload.nfo_fields // {}) as $nfo
  | if (($nfo["@original@"][0] | nonempty) and ($nfo["@display@"][0] | nonempty)) then
      $nfo["@display@"][0]
    elif $source.translations_complete != true then
      error("Translation choices are incomplete")
    else
      ([ $source.translations[]
         | select(.target_language == "en" and (.source_fields | index("@field@"))) ]
       | sort_by(.captured_at, .evidence_uuid)
       | last | .translated_text | select(nonempty))
      // ($source.metadata.@field@ | readable_text | select(nonempty))
      // empty
    end
  end`

const nativeDisplayMapping = `def nonempty: type == "string" and length > 0;
.source as $source
| if $source == null then empty
  else ($source.payload.display_translations // {}) as $saved
  | if (($saved | has("@field@")) and $saved.@field@ == null) then
      $source.metadata.@field@
    elif $source.translations_complete != true then
      error("Translation choices are incomplete")
    elif ($saved | has("@field@")) then
      [ $source.translations[]
        | select(.uuid == $saved.@field@ and (.source_fields | index("@field@"))) ]
      | if length != 1 then error("Saved display translation is unavailable")
        else .[0].translated_text end
    else
      ([ $source.translations[]
         | select(.target_language == "en" and (.source_fields | index("@field@"))) ]
       | sort_by(.captured_at, .evidence_uuid)
       | last | .translated_text | select(nonempty))
      // ($source.metadata.@field@ | readable_text | select(nonempty))
      // empty
    end
  end`

func compactNFOPolicies(ctx context.Context, result *NFOCleanupResult) error {
	store := &MetadataPolicyStore{}
	after := ""
	for {
		var ids []string
		if err := dbWrapper.Select(ctx, &ids, `SELECT p.collection_uuid FROM metadata_policies p
JOIN metadata_policy_revisions r ON r.collection_uuid=p.collection_uuid AND r.revision=p.revision
WHERE p.collection_uuid>? AND (instr(r.definition,'nfo_fields')>0 OR instr(r.definition,'nfo_path')>0)
ORDER BY p.collection_uuid LIMIT 100`, after); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			policy, err := store.Find(ctx, id)
			if err != nil {
				return err
			}
			changed := false
			for kind, rule := range policy.Definition.Rules {
				for field, mapping := range rule.Mappings {
					if !strings.Contains(mapping.JQ, "nfo_fields") && !strings.Contains(mapping.JQ, "nfo_path") {
						continue
					}
					original, display, sourceField := "original-title", "title", "title"
					if field == "details" {
						original, display, sourceField = "original-plot", "plot", "original_text"
					} else if field != "title" {
						return fmt.Errorf("policy %s has a custom mapping that reads discarded NFO inputs", id)
					}
					replace := strings.NewReplacer("@original@", original, "@display@", display, "@field@", sourceField)
					if strings.TrimSpace(mapping.JQ) != replace.Replace(oldNFODisplayMapping) {
						return fmt.Errorf("policy %s has a custom %s mapping that reads discarded NFO inputs", id, field)
					}
					mapping.JQ = replace.Replace(nativeDisplayMapping)
					rule.Mappings[field] = mapping
					changed = true
				}
				policy.Definition.Rules[kind] = rule
			}
			if !changed {
				continue
			}
			collection, err := (&SourceCollectionStore{}).Find(ctx, id)
			if err != nil {
				return err
			}
			if _, err := store.Put(ctx, models.MetadataPolicyInput{
				CollectionUUID: id, ExpectedRevision: policy.Revision, ExpectedCollectionRevision: collection.Revision,
				Definition: policy.Definition, Origin: "migration", Reason: "Use shared native display translations after imported document cleanup",
			}); err != nil {
				return err
			}
			result.Policies++
		}
		after = ids[len(ids)-1]
	}
}
