import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import { createArchiveRequest, nativeArchiveEndpoint } from "./client";

export const sourceStudioSchema = z.object({ uuid, name: z.string() });
export const sourceStudioAssignmentSchema = z.object({
  collection_uuid: uuid,
  revision: z.number().int().nonnegative(),
  studio: sourceStudioSchema.nullable(),
});
export type SourceStudio = z.infer<typeof sourceStudioSchema>;
export type SourceStudioAssignment = z.infer<
  typeof sourceStudioAssignmentSchema
>;

export function createSourceStudioAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  return {
    assignment: (collection: string, signal?: AbortSignal) =>
      request(
        `collections/${uuid.parse(collection)}/studio`,
        sourceStudioAssignmentSchema,
        undefined,
        signal,
      ),
    save: (current: SourceStudioAssignment, studio: SourceStudio | null) =>
      request(
        `collections/${uuid.parse(current.collection_uuid)}/studio`,
        sourceStudioAssignmentSchema,
        JSON.stringify({
          expected_revision: current.revision,
          studio_uuid: studio?.uuid ?? null,
        }),
        undefined,
        "PUT",
      ),
    post: (post: string, signal?: AbortSignal) =>
      request(
        `posts/${uuid.parse(post)}/studios`,
        z.array(sourceStudioSchema),
        undefined,
        signal,
      ),
  };
}
