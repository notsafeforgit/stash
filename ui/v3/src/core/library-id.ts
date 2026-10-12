import { z } from "zod";

export const libraryIdPattern = /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/;
export const libraryIdSchema = z
  .string()
  .regex(libraryIdPattern)
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000");
export const isLibraryId = (value: string) =>
  libraryIdSchema.safeParse(value).success;

// Only old bookmarks and already-downloaded offline records use this decoder.
// Resolve legacy references before calling the current library API.
export const retainedLibraryIdSchema = z.union([
  libraryIdSchema,
  z.string().regex(/^[1-9]\d*$/),
]);
