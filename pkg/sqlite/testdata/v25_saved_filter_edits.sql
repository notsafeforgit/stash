-- Writes using only the mainline v2.5 saved_filters schema. The old server
-- knows nothing about fork_saved_filter_state and must not need to update it.
UPDATE saved_filters
SET name = 'Flat edited in v2.5',
    object_filter = '{"rating100":{"value":90,"modifier":"GREATER_THAN"}}'
WHERE name = 'Flat';

UPDATE saved_filters
SET object_filter = '{"rating100":{"value":60,"modifier":"LESS_THAN"}}'
WHERE name = 'Complex';

DELETE FROM saved_filters WHERE name = 'Removed';

INSERT INTO saved_filters (name, mode, find_filter, object_filter, ui_options)
VALUES ('Created in v2.5', 'SCENES', '', '{"organized":true}', '');
