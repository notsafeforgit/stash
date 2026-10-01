-- A path's removal history is independent of the lifetime of any file UUID.
-- Do not infer removals that happened before this migration.
CREATE TABLE file_path_fences (
 folder_path TEXT NOT NULL,
 basename TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(typeof(revision)='integer' AND revision>0),
 PRIMARY KEY(folder_path,basename)
);
CREATE INDEX file_path_fences_folded ON file_path_fences(folder_path COLLATE NOCASE,basename COLLATE NOCASE,revision);
CREATE TRIGGER file_path_fence_forward BEFORE UPDATE ON file_path_fences
WHEN NEW.folder_path!=OLD.folder_path OR NEW.basename!=OLD.basename OR NEW.revision!=OLD.revision+1
BEGIN SELECT RAISE(ABORT,'file path removal fence must advance'); END;

CREATE TRIGGER file_path_removed BEFORE DELETE ON files
WHEN OLD.zip_file_id IS NULL
BEGIN
 INSERT INTO file_path_fences(folder_path,basename,revision)
 SELECT path,OLD.basename,1 FROM folders WHERE id=OLD.parent_folder_id
 ON CONFLICT(folder_path,basename) DO UPDATE SET revision=revision+1;
END;
CREATE TRIGGER file_path_moved BEFORE UPDATE OF basename,parent_folder_id,zip_file_id ON files
WHEN OLD.zip_file_id IS NULL AND
 (NEW.basename IS NOT OLD.basename OR NEW.parent_folder_id IS NOT OLD.parent_folder_id OR NEW.zip_file_id IS NOT OLD.zip_file_id)
BEGIN
 INSERT INTO file_path_fences(folder_path,basename,revision)
 SELECT path,OLD.basename,1 FROM folders WHERE id=OLD.parent_folder_id
 ON CONFLICT(folder_path,basename) DO UPDATE SET revision=revision+1;
END;
CREATE TRIGGER file_path_folder_moved BEFORE UPDATE OF path ON folders
WHEN NEW.path IS NOT OLD.path
BEGIN
 INSERT INTO file_path_fences(folder_path,basename,revision)
 SELECT OLD.path,basename,1 FROM files WHERE parent_folder_id=OLD.id AND zip_file_id IS NULL
 ON CONFLICT(folder_path,basename) DO UPDATE SET revision=revision+1;
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000020,'File path removal fences for delayed intake','{}');
