const ROTATABLE_VIDEO_EXTENSIONS = [".mkv", ".mp4", ".m4v", ".mov"];

export function supportsSceneVideoRotation(
  path: string | null | undefined,
): boolean {
  if (!path) return false;
  const normalizedPath = path.toLowerCase();
  return ROTATABLE_VIDEO_EXTENSIONS.some((extension) =>
    normalizedPath.endsWith(extension),
  );
}
