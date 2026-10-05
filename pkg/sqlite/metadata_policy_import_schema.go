package sqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

func validateMetadataPolicyImportSchema(conn *sqlx.DB) error {
	for _, name := range []string{"metadata_policy_imports", "metadata_policy_import_collection", "metadata_policy_import_immutable", "metadata_policy_import_documents", "metadata_policy_import_document_immutable", "metadata_policy_import_document_source"} {
		var found bool
		if err := conn.Get(&found, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)`, name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM metadata_policy_imports i
LEFT JOIN metadata_policy_revisions p ON p.collection_uuid=i.collection_uuid AND p.revision=i.policy_revision
WHERE p.collection_uuid IS NULL OR p.collection_revision!=i.collection_revision
 OR i.uuid IS NOT json_extract(i.plan,'$.uuid') OR i.uuid IS NOT json_extract(i.binding,'$.uuid')
 OR i.collection_uuid IS NOT json_extract(i.plan,'$.collection.uuid') OR i.collection_uuid IS NOT json_extract(i.binding,'$.policy.collection_uuid')
 OR i.collection_revision IS NOT json_extract(i.plan,'$.collection.revision') OR i.collection_revision IS NOT json_extract(i.binding,'$.policy.expected_collection_revision')
 OR i.input_sha256 IS NOT json_extract(i.plan,'$.input_sha256') OR i.plan_sha256 IS NOT json_extract(i.plan,'$.plan_sha256')
 OR json(p.definition) IS NOT json_extract(i.plan,'$.definition')
 OR coalesce(json_array_length(i.plan,'$.folder_sources'),0)!=(SELECT count(*) FROM metadata_policy_import_documents d WHERE d.import_uuid=i.uuid)
 OR EXISTS(SELECT 1 FROM json_each(i.plan,'$.folder_sources') s WHERE NOT EXISTS(
  SELECT 1 FROM metadata_policy_import_documents d JOIN source_document_head_decisions h ON h.uuid=d.head_uuid
  WHERE d.import_uuid=i.uuid AND d.source_uuid=json_extract(s.value,'$.source_uuid') AND d.head_uuid=json_extract(s.value,'$.head_uuid')
   AND h.source_uuid=d.source_uuid AND h.state='linked')))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native metadata policy migration references are incomplete")
	}
	// Decode one bounded receipt at a time. Retained source values and plan hashes
	// are part of restore integrity, even after the current policy changes.
	rows, err := conn.Queryx(`SELECT binding,plan,input_sha256,plan_sha256,created_at FROM metadata_policy_imports ORDER BY uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var binding, planBytes []byte
		var inputHash, planHash, created string
		if err := rows.Scan(&binding, &planBytes, &inputHash, &planHash, &created); err != nil {
			return err
		}
		var input models.MetadataPolicyImportInput
		var plan models.MetadataPolicyImportPlan
		now, err := time.Parse(time.RFC3339Nano, created)
		if err != nil || json.Unmarshal(binding, &input) != nil || json.Unmarshal(planBytes, &plan) != nil {
			return errors.New("native metadata policy migration receipt is invalid")
		}
		if !reflect.DeepEqual(input.Policy.Definition, plan.Definition) {
			return errors.New("native metadata policy migration definition differs")
		}
		_, calculatedInput, _, err := metadata.PreparePolicyImport(input, now)
		if err != nil || calculatedInput != inputHash {
			return errors.New("native metadata policy migration source digest differs")
		}
		calculatedPlan, err := metadata.PolicyImportPlanDigest(plan)
		if err != nil || calculatedPlan != planHash {
			return errors.New("native metadata policy migration plan digest differs")
		}
	}
	return rows.Err()
}
