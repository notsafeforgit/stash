-- Field choices decide whether automatic metadata may change a live field. A
-- deleted scene or image has no fields left, so its choices and baseline go,
-- now and whenever one is deleted. The deletion marker stays: intake reads it
-- to keep the item from being imported again.
INSERT INTO native_migration_history(version,name,details)
VALUES(1000146,'Retire deleted media field choices',json_object(
 'items',(SELECT count(DISTINCT d.entity_uuid) FROM metadata_field_decisions d
  JOIN archive_entities a ON a.uuid=d.entity_uuid WHERE a.state='deleted' AND a.kind IN ('scene','image')),
 'choices',(SELECT count(*) FROM metadata_field_decisions d
  JOIN archive_entities a ON a.uuid=d.entity_uuid WHERE a.state='deleted' AND a.kind IN ('scene','image')),
 'baselines',(SELECT count(*) FROM metadata_field_baselines b
  JOIN archive_entities a ON a.uuid=b.entity_uuid WHERE a.state='deleted' AND a.kind IN ('scene','image'))));

-- metadata_field_decision_cleanup removes each choice's references, policy
-- links and post-media links.
DELETE FROM metadata_field_heads WHERE entity_uuid IN
 (SELECT uuid FROM archive_entities WHERE state='deleted' AND kind IN ('scene','image'));
DELETE FROM metadata_field_decisions WHERE entity_uuid IN
 (SELECT uuid FROM archive_entities WHERE state='deleted' AND kind IN ('scene','image'));
DELETE FROM metadata_field_baselines WHERE entity_uuid IN
 (SELECT uuid FROM archive_entities WHERE state='deleted' AND kind IN ('scene','image'));
DELETE FROM metadata_field_pending WHERE entity_uuid IN
 (SELECT uuid FROM archive_entities WHERE state='deleted' AND kind IN ('scene','image'));

CREATE TRIGGER metadata_media_retired AFTER UPDATE OF state ON archive_entities
WHEN OLD.state='active' AND NEW.state='deleted' AND NEW.kind IN ('scene','image')
BEGIN
  DELETE FROM metadata_field_heads WHERE entity_uuid=NEW.uuid;
  DELETE FROM metadata_field_decisions WHERE entity_uuid=NEW.uuid;
  DELETE FROM metadata_field_baselines WHERE entity_uuid=NEW.uuid;
  DELETE FROM metadata_field_pending WHERE entity_uuid=NEW.uuid;
END;
