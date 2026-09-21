import { z } from "zod";

const localURL = z
  .string()
  .refine(
    (value) =>
      value.startsWith("/") && !value.startsWith("//") && !value.includes("\\"),
  );
const optionalURL = z.union([z.literal(""), localURL]);
const kind = z.enum(["SCENE", "IMAGE", "GALLERY"]);
const mediaSchema = z.object({
  key: z.string().regex(/^(scene|image)-[1-9][0-9]*$/),
  kind: z.enum(["SCENE", "IMAGE"]),
  title: z.string(),
  width: z.number().int().nonnegative(),
  height: z.number().int().nonnegative(),
  duration: z.number().nonnegative(),
  video: z.boolean(),
  thumbnail: localURL,
  image: localURL,
  download: optionalURL,
});
export const shareStatusSchema = z.object({
  expires_at: z.iso.datetime(),
  server_time: z.iso.datetime(),
});
export const shareContentSchema = shareStatusSchema.extend({
  label: z.string(),
  entries: z.array(
    z.object({ kind, title: z.string(), media_keys: z.array(z.string()) }),
  ),
  media: z.array(mediaSchema),
});
export const shareDetailSchema = z.object({
  media: mediaSchema,
  video_codec: z.string(),
  audio_codec: z.string(),
  frame_rate: z.number().nonnegative(),
  streams: z.array(
    z.object({
      url: localURL,
      mime_type: z.string().nullable(),
      label: z.string().nullable(),
    }),
  ),
});
export type SharedContent = z.infer<typeof shareContentSchema>;
export type SharedMedia = z.infer<typeof mediaSchema>;
export type SharedDetail = z.infer<typeof shareDetailSchema>;

export class ShareUnavailableError extends Error {}

export async function shareRequest<T>(
  url: URL,
  schema: z.ZodType<T>,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(url, {
    credentials: "same-origin",
    cache: "no-store",
    redirect: "error",
    signal,
  });
  if (
    response.status === 404 ||
    response.status === 401 ||
    response.status === 403
  )
    throw new ShareUnavailableError();
  if (!response.ok) throw new Error("Unable to load shared media");
  const value: unknown = await response.json();
  return schema.parse(value);
}

export function shareTarget(
  href: string,
  baseHref: string,
): { base: URL; secret: string } | null {
  const location = new URL(href);
  const root = new URL(baseHref);
  if (
    location.origin !== root.origin ||
    !location.pathname.startsWith(root.pathname)
  )
    return null;
  const id = location.pathname.slice(root.pathname.length).replace(/\/$/, "");
  if (!/^[A-Za-z0-9_-]{22}$/.test(id)) return null;
  return { base: new URL(`${id}/`, root), secret: location.hash.slice(1) };
}

export function shareDeadline(
  status: z.infer<typeof shareStatusSchema>,
  now = Date.now(),
) {
  return (
    now +
    Math.max(0, Date.parse(status.expires_at) - Date.parse(status.server_time))
  );
}
