package sqlite

import "context"

const catalogCaptureCandidates = `source_table IN ('observations','observation_details') AND outcome!='shared'`

type catalogCaptureSource struct {
	Ordinal int64   `db:"ordinal"`
	Post    *string `db:"post_uuid"`
	Capture *string `db:"capture_uuid"`
	Reason  string  `db:"reason"`
}

func catalogCapturePayloadBytes(ctx context.Context, id string) (int64, error) {
	var size int64
	// Count retained parts conservatively, including repeated profile references.
	err := dbWrapper.Get(ctx, &size, `SELECT shared.byte_length+patch.byte_length+coalesce((
 SELECT sum(p.byte_length) FROM source_capture_profiles ref JOIN source_profile_bodies b ON b.hash=ref.profile_hash
 JOIN source_payloads p ON p.digest=b.payload_digest WHERE ref.capture_uuid=c.uuid),0)
 FROM source_captures c JOIN source_post_revisions r ON r.uuid=c.revision_uuid
 JOIN source_payloads shared ON shared.digest=r.body_digest JOIN source_payloads patch ON patch.digest=c.patch_digest WHERE c.uuid=?`, id)
	return size, err
}
