-- Funscript support is retired. Scans no longer look for a .funscript beside
-- a video, and heatmap generation is gone, so nothing writes or reads a video
-- file's interactive flag or its heatmap-derived speed. The counts record what
-- the columns held when they were dropped.
INSERT INTO native_migration_history(version,name,details)
VALUES(1000145,'Retire funscript file fields',json_object(
 'interactive_files',(SELECT count(*) FROM video_files WHERE interactive),
 'files_with_speed',(SELECT count(*) FROM video_files WHERE interactive_speed IS NOT NULL)));

ALTER TABLE video_files DROP COLUMN interactive;
ALTER TABLE video_files DROP COLUMN interactive_speed;
