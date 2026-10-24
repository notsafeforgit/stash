-- Search caches may survive restarts only when this transactional token and
-- the schema version match. A random token also distinguishes divergent writes
-- after restoring an older library; an integer counter alone cannot do that.
CREATE TABLE library_search_state (
 id INTEGER PRIMARY KEY CHECK(id=1),
 token TEXT NOT NULL CHECK(length(token)=32 AND token NOT GLOB '*[^0-9a-f]*')
);
INSERT INTO library_search_state VALUES (1,
 CASE WHEN EXISTS(SELECT 1 FROM scenes) OR EXISTS(SELECT 1 FROM images)
 THEN lower(hex(randomblob(16))) ELSE '00000000000000000000000000000000' END);
CREATE TRIGGER library_search_scenes_insert AFTER INSERT ON scenes
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scenes_delete AFTER DELETE ON scenes
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scenes_update AFTER UPDATE OF id, title, details ON scenes
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scenes_files_insert AFTER INSERT ON scenes_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scenes_files_delete AFTER DELETE ON scenes_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scenes_files_update AFTER UPDATE OF scene_id, file_id ON scenes_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_insert AFTER INSERT ON images
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_delete AFTER DELETE ON images
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_update AFTER UPDATE OF id, title, details ON images
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_files_insert AFTER INSERT ON images_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_files_delete AFTER DELETE ON images_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_images_files_update AFTER UPDATE OF image_id, file_id ON images_files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_files_update AFTER UPDATE OF id, basename, parent_folder_id ON files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_files_delete AFTER DELETE ON files
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_files_fingerprints_insert AFTER INSERT ON files_fingerprints
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_files_fingerprints_delete AFTER DELETE ON files_fingerprints
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_files_fingerprints_update AFTER UPDATE ON files_fingerprints
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_folders_update AFTER UPDATE OF id, path ON folders
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scene_markers_insert AFTER INSERT ON scene_markers
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scene_markers_delete AFTER DELETE ON scene_markers
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_scene_markers_update AFTER UPDATE OF scene_id, title ON scene_markers
BEGIN UPDATE library_search_state SET token=lower(hex(randomblob(16))) WHERE id=1; END;
CREATE TRIGGER library_search_state_retained BEFORE DELETE ON library_search_state
BEGIN SELECT RAISE(ABORT,'search revision state is required'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000131,'Transactional search revision for persistent read indexes','{}');
