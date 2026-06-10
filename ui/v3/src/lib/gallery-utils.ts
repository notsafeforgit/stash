import { fileStemFromPath } from "src/utils/file";

export type GalleryLabelable = {
  id?: string;
  title?: string | null;
  files?: Array<{ path: string }>;
  folder?: { path: string; basename?: string | null } | null;
};

/**
 * Returns a human-readable label for a gallery, falling back from title →
 * folder basename → folder path basename → first file stem (sans extension)
 * → id. Uses `||` (not `??`) so empty-string titles are treated as absent.
 */
export function galleryLabel(g: GalleryLabelable): string {
  if (g.title) return g.title;
  if (g.folder?.basename) return g.folder.basename;
  if (g.folder?.path)
    return g.folder.path.split(/[\\/]/).pop() ?? g.folder.path;
  if (g.files?.[0]?.path) return fileStemFromPath(g.files[0].path);
  return g.id ?? "";
}
