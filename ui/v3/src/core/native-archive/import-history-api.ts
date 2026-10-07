import { z } from "zod";
import { accountUUIDSchema as uuid } from "./account-review-api";
import {
  createArchiveRequest,
  nativeArchiveEndpoint,
  NativeArchiveError,
} from "./client";

export const importKindSchema = z.enum(["catalog", "automation"]);
export const importHistorySearchSchema = z.object({
  kind: importKindSchema.default("catalog"),
  snapshot: uuid.optional(),
});
export type ImportHistorySearch = z.infer<typeof importHistorySearchSchema>;
export type ImportKind = z.infer<typeof importKindSchema>;

const count = z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER);
const timestamp = z.iso.datetime({ offset: true });
const snapshotSchema = z
  .object({
    kind: importKindSchema,
    uuid,
    source_uuid: uuid,
    registry_import_uuid: uuid,
    collection_uuid: uuid.nullable(),
    collection_label: z.string().nullable(),
    captured_at: timestamp,
    transfer_state: z.enum(["receiving", "received"]),
    chunks: count,
    received_chunks: count,
    records: count,
    received_records: count,
    bytes: count,
    received_bytes: count,
    created_at: timestamp,
    updated_at: timestamp,
  })
  .refine(
    (row) =>
      row.received_chunks <= row.chunks &&
      row.received_records <= row.records &&
      row.received_bytes <= row.bytes &&
      (row.transfer_state === "received") ===
        (row.received_chunks === row.chunks) &&
      (row.transfer_state !== "received" ||
        (row.received_records === row.records &&
          row.received_bytes === row.bytes)) &&
      (row.kind === "catalog"
        ? row.collection_uuid !== null &&
          row.collection_label !== null &&
          row.chunks > 0 &&
          row.records > 0 &&
          row.bytes > 0
        : row.collection_uuid === null && row.collection_label === null),
    "Inconsistent snapshot transfer",
  );

export const importFamilies = {
  catalog: [
    "evidence",
    "relations",
    "publishers",
    "attachments",
    "media",
    "memberships",
    "documents",
    "translations",
    "enrichment",
    "file_history",
    "cleanup",
  ],
  automation: ["translations", "enrichment", "discovery", "checkpoints"],
} as const;
const progressSchema = z
  .object({
    state: z.enum(["running", "mapped", "review", "retained"]),
    source_records: count,
    processed_records: count,
    historical_review_records: count,
    updated_at: timestamp,
  })
  .refine(
    (row) =>
      row.processed_records <= row.source_records &&
      row.historical_review_records <= row.processed_records &&
      (row.state === "running" ||
        row.processed_records === row.source_records) &&
      (row.state !== "review" || row.historical_review_records > 0) &&
      ((row.state !== "mapped" && row.state !== "retained") ||
        row.historical_review_records === 0),
    "Inconsistent historical import progress",
  );
const detailSchema = z
  .object({
    snapshot: snapshotSchema,
    families: z
      .array(
        z.object({
          name: z.enum([...importFamilies.catalog, "discovery", "checkpoints"]),
          progress: progressSchema.nullable(),
        }),
      )
      .max(11),
  })
  .refine((row) => {
    const expected = importFamilies[row.snapshot.kind];
    return (
      row.families.length === expected.length &&
      row.families.every((family, index) => family.name === expected[index])
    );
  }, "Unexpected import families");

export type ImportSnapshot = z.infer<typeof snapshotSchema>;
export type ImportDetails = z.infer<typeof detailSchema>;

export function createImportHistoryAPI(
  endpoint = nativeArchiveEndpoint(),
  transport: typeof fetch = fetch,
) {
  const request = createArchiveRequest(endpoint, transport);
  const pageLimit = 25;
  async function snapshots(kind: ImportKind, after = "", signal?: AbortSignal) {
    importKindSchema.parse(kind);
    const query = new URLSearchParams({ limit: String(pageLimit) });
    if (after) query.set("after", uuid.parse(after));
    const rows = await request(
      `import-history/${kind}?${query}`,
      z.array(snapshotSchema).max(pageLimit),
      undefined,
      signal,
    );
    let previous = after;
    for (const row of rows) {
      if (row.kind !== kind || row.uuid <= previous)
        throw new NativeArchiveError(0, "invalid_response");
      previous = row.uuid;
    }
    return rows;
  }
  async function snapshot(kind: ImportKind, id: string, signal?: AbortSignal) {
    importKindSchema.parse(kind);
    const row = await request(
      `import-history/${kind}/${uuid.parse(id)}`,
      detailSchema,
      undefined,
      signal,
    );
    if (row.snapshot.kind !== kind || row.snapshot.uuid !== id)
      throw new NativeArchiveError(0, "invalid_response");
    return row;
  }
  return { endpoint, pageLimit, snapshots, snapshot };
}

export type ImportHistoryAPI = ReturnType<typeof createImportHistoryAPI>;
