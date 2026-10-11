package sqlite

// Select actual unresolved posts before applying pagination. The candidate
// indexes remain the starting point, but settled candidates no longer produce
// empty pages or inflate the review count. The same selection serves totals.
// Materialize reused sets so correlated link checks do not rebuild them per row.
func archiveReviewMediaItemsQuery(after string, limit int) (string, []any) {
	candidates, args := archiveReviewMediaQuery(after, -1)
	query := `WITH RECURSIVE candidates AS (` + candidates + `),
identities AS MATERIALIZED (
 SELECT i.post_uuid,i.canonical_uuid FROM candidates c
 JOIN source_posts p ON p.uuid=c.post_uuid AND p.state='active'
 JOIN source_post_identities i ON i.canonical_uuid=c.post_uuid
), raw_media AS MATERIALIZED (
 SELECT i.canonical_uuid AS post_uuid,e.media_uuid FROM identities i JOIN source_media_evidence e ON e.post_uuid=i.post_uuid
 UNION SELECT i.canonical_uuid,l.media_uuid FROM identities i JOIN post_media_links l ON l.post_uuid=i.post_uuid
 UNION SELECT i.canonical_uuid,d.media_uuid FROM identities i JOIN source_attachments a ON a.post_uuid=i.post_uuid
 JOIN attachment_media_links l ON l.attachment_uuid=a.uuid JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid WHERE d.media_uuid IS NOT NULL
), resolved(post_uuid,requested,uuid,depth) AS (
 SELECT post_uuid,media_uuid,media_uuid,0 FROM raw_media
 UNION ALL SELECT r.post_uuid,r.requested,e.redirect_to,r.depth+1 FROM resolved r JOIN archive_entities e ON e.uuid=r.uuid
 WHERE e.state='redirected' AND r.depth<127
), media AS MATERIALIZED (
 SELECT r.post_uuid,r.requested,r.uuid FROM resolved r JOIN archive_entities e ON e.uuid=r.uuid
 WHERE e.state='active' AND e.kind IN ('image','scene')
), choices AS MATERIALIZED (
 SELECT i.canonical_uuid AS post_uuid,d.media_uuid,d.state FROM identities i JOIN post_media_links l ON l.post_uuid=i.post_uuid
 JOIN post_media_decisions d ON d.uuid=l.decision_uuid
), states AS MATERIALIZED (
 SELECT m.post_uuid,m.uuid,count(DISTINCT c.state) AS states,max(c.state) AS state FROM media m
 LEFT JOIN choices c ON c.post_uuid=m.post_uuid AND c.media_uuid=m.requested GROUP BY m.post_uuid,m.uuid
), attachments AS MATERIALIZED (
 SELECT i.canonical_uuid AS post_uuid,a.namespace,a.value,count(*) AS count,count(DISTINCT d.state) AS states,
 max(d.state) AS state,max(d.media_uuid) AS media FROM identities i JOIN source_attachments a ON a.post_uuid=i.post_uuid
 JOIN attachment_media_links l ON l.attachment_uuid=a.uuid JOIN attachment_media_decisions d ON d.uuid=l.decision_uuid
 GROUP BY i.canonical_uuid,a.namespace,a.value
), linked AS MATERIALIZED (
 SELECT a.post_uuid,m.uuid FROM attachments a JOIN media m ON m.post_uuid=a.post_uuid AND m.requested=a.media
 WHERE a.count=1 AND a.state='linked'
), needed AS (
 SELECT post_uuid FROM attachments WHERE (count>1 AND (states>1 OR state!='unlinked')) OR state='undecided'
 UNION SELECT s.post_uuid FROM states s WHERE s.states>1 OR
 (coalesce(s.state,'undecided')='undecided' AND NOT EXISTS(SELECT 1 FROM linked l WHERE l.post_uuid=s.post_uuid AND l.uuid=s.uuid))
 UNION SELECT post_uuid FROM raw_media GROUP BY post_uuid HAVING count(*)>8192
 UNION SELECT post_uuid FROM choices GROUP BY post_uuid HAVING count(*)>8192
 UNION SELECT post_uuid FROM attachments GROUP BY post_uuid HAVING count(*)>8192
)
SELECT post_uuid FROM needed ORDER BY post_uuid LIMIT ?`
	return query, append(args, limit)
}
