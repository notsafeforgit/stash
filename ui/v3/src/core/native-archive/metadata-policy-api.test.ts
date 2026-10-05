import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { IDBFactory, IDBObjectStore } from "fake-indexeddb";
import {
  createMetadataPolicyAPI,
  policyInputSchema,
  policyDraftInputSchema,
  type PolicyInput,
  type MetadataPolicy,
  type PolicyDraftInput,
} from "./metadata-policy-api";
import { createMetadataPolicyOutbox } from "./metadata-policy-outbox";
import {
  collection,
  collectionID,
  rootID,
} from "../../../tests/fixtures/collections";

const endpoint = "https://example.test/stash/api/v3/archive/";
beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("preserves bounded validation detail without turning it into a successful response", async () => {
  const api = createMetadataPolicyAPI(endpoint, async () =>
    Response.json(
      {
        error: "invalid_policy",
        message: "scene.title: invalid jq expression",
      },
      { status: 400 },
    ),
  );
  await expect(api.save(JSON.stringify(input()))).rejects.toMatchObject({
    status: 400,
    code: "invalid_policy",
    detail: "scene.title: invalid jq expression",
  });
});

function input(): PolicyInput {
  return {
    collection_uuid: collectionID,
    expected_revision: 0,
    expected_collection_revision: 1,
    reason: "Assign this folder",
    definition: {
      enabled: false,
      apply_to_scans: true,
      rules: {
        scene: {
          on_create: true,
          on_existing: false,
          skip_organized_on_create: false,
          mark_organized: false,
          filename_title_fallback: true,
          mappings: {
            title: { jq: ".source.metadata.title // empty" },
            performers: { value: [rootID] },
          },
        },
      },
    },
  };
}
function server() {
  let current: MetadataPolicy | null = null;
  const history: MetadataPolicy[] = [];
  let collectionRevision = 1;
  let loseAfter = false;
  let loseBefore = false;
  const bodies: string[] = [];
  function commit(next: PolicyInput) {
    current = {
      collection_uuid: collectionID,
      revision: next.expected_revision + 1,
      collection_revision: next.expected_collection_revision,
      definition: next.definition,
      origin: "review",
      reason: next.reason,
      created_at: "2026-10-05T20:00:00Z",
    };
    history.push(current);
  }
  const transport = vi.fn<typeof fetch>(async (target, options) => {
    const url = new URL(String(target));
    if (options?.method === "PUT") {
      bodies.push(String(options.body));
      if (loseBefore) {
        loseBefore = false;
        throw new TypeError("lost before commit");
      }
      const next = policyInputSchema.parse(JSON.parse(String(options.body)));
      if (
        next.expected_revision !== (current?.revision ?? 0) ||
        next.expected_collection_revision !== collectionRevision
      )
        return Response.json({ error: "preview_changed" }, { status: 409 });
      commit(next);
      if (loseAfter) {
        loseAfter = false;
        throw new TypeError("lost after commit");
      }
      return Response.json(current);
    }
    if (url.pathname.endsWith("/history"))
      return Response.json(
        history
          .filter((row) => row.revision > Number(url.searchParams.get("after")))
          .slice(0, Number(url.searchParams.get("limit"))),
      );
    if (url.pathname.endsWith("/metadata-policy"))
      return Response.json(current);
    return Response.json({ ...collection(), revision: collectionRevision });
  });
  return {
    api: createMetadataPolicyAPI(endpoint, transport),
    transport,
    commit,
    bodies,
    loseAfter: () => {
      loseAfter = true;
    },
    loseBefore: () => {
      loseBefore = true;
    },
    editCollection: () => {
      collectionRevision++;
    },
  };
}

it("recovers a lost policy save from immutable history even after another policy edit", async () => {
  const remote = server();
  const outbox = createMetadataPolicyOutbox(remote.api);
  remote.loseAfter();
  await outbox.prepare(input());
  await expect(outbox.deliver(collectionID)).rejects.toThrow("lost after");
  remote.commit({
    ...input(),
    expected_revision: 1,
    definition: { ...input().definition, enabled: true },
  });
  remote.editCollection();
  const reloaded = createMetadataPolicyOutbox(remote.api);
  expect(await reloaded.deliver(collectionID)).toMatchObject({
    revision: 1,
    definition: { enabled: false },
  });
  expect(remote.bodies).toHaveLength(1);
  expect(await reloaded.read(collectionID)).toBeNull();
});

it("replays exact bytes and serializes competing tabs before sending", async () => {
  const remote = server();
  const one = createMetadataPolicyOutbox(remote.api);
  remote.loseBefore();
  const saved = await one.prepare(input());
  await expect(one.deliver(collectionID)).rejects.toThrow("lost before");
  const two = createMetadataPolicyOutbox(remote.api);
  await expect(
    two.prepare({ ...input(), reason: "Another choice" }),
  ).rejects.toMatchObject({ code: "pending_review" });
  await two.deliver(collectionID);
  expect(remote.bodies).toEqual([saved.body, saved.body]);
});

it("requires a fresh review when the collection changed, including an otherwise unchanged policy", async () => {
  const remote = server();
  remote.commit(input());
  remote.editCollection();
  const outbox = createMetadataPolicyOutbox(remote.api);
  const saved = await outbox.prepare({ ...input(), expected_revision: 1 });
  await expect(outbox.deliver(collectionID)).rejects.toMatchObject({
    code: "preview_changed",
  });
  expect((await outbox.read(collectionID))?.state).toBe("rejected");
  expect(remote.bodies).toEqual([]);
  await outbox.forgetRejected(collectionID, saved.body);
});

it("rebinds an unchanged policy to a newly reviewed collection revision", async () => {
  const remote = server();
  remote.commit(input());
  remote.editCollection();
  const outbox = createMetadataPolicyOutbox(remote.api);
  await outbox.prepare({
    ...input(),
    expected_revision: 1,
    expected_collection_revision: 2,
  });
  expect(await outbox.deliver(collectionID)).toMatchObject({
    revision: 2,
    collection_revision: 2,
  });
  expect(remote.bodies).toHaveLength(1);
});

it("requires durable storage and isolates two mounts on the same origin", async () => {
  const remote = server();
  const outbox = createMetadataPolicyOutbox(remote.api);
  const add = vi
    .spyOn(IDBObjectStore.prototype, "add")
    .mockImplementation(() => {
      throw new Error("disk full");
    });
  await expect(outbox.prepare(input())).rejects.toThrow("disk full");
  expect(remote.transport).not.toHaveBeenCalled();
  add.mockRestore();
  await outbox.prepare(input());
  const other = createMetadataPolicyOutbox(
    createMetadataPolicyAPI(
      "https://example.test/other/api/v3/archive/",
      remote.transport,
    ),
  );
  expect(await other.read(collectionID)).toBeNull();
});

it("keeps jq as plain text and rejects leaked settings, duplicate value modes and simulated Apply fields", async () => {
  const sample: PolicyDraftInput = {
    collection_uuid: collectionID,
    expected_collection_revision: 1,
    expected_policy_revision: 0,
    definition: input().definition,
    entity_uuid: rootID,
    file_uuid: collectionID,
    event: "create",
    include_data: true,
  };
  const transport = vi.fn<typeof fetch>(async () =>
    Response.json({
      context: {
        collection_uuid: collectionID,
        collection_revision: 1,
        policy_revision: 0,
        entity_uuid: rootID,
        expected_entity_revision: 1,
        relative_path: "Purchased/file.mp4",
        created: true,
      },
      state: "ready",
      changes: [],
    }),
  );
  await createMetadataPolicyAPI(endpoint, transport).draft(sample);
  const sent = JSON.parse(String(transport.mock.calls[0]?.[1]?.body));
  expect(sent.definition.rules.scene.mappings.title.jq).toBe(
    ".source.metadata.title // empty",
  );
  expect(
    policyDraftInputSchema.safeParse({ ...sample, settings: {} }).success,
  ).toBe(false);
  expect(
    policyDraftInputSchema.safeParse({ ...sample, digest: "a".repeat(64) })
      .success,
  ).toBe(false);
  const invalid = input();
  if (invalid.definition.rules?.scene)
    invalid.definition.rules.scene.mappings = {
      title: { jq: ".entity.title", value: "Bad" },
    };
  expect(policyInputSchema.safeParse(invalid).success).toBe(false);
  transport.mockResolvedValueOnce(
    Response.json({
      context: sent,
      state: "ready",
      changes: [],
      digest: "a".repeat(64),
    }),
  );
  await expect(
    createMetadataPolicyAPI(endpoint, transport).draft(sample),
  ).rejects.toMatchObject({ code: "invalid_response" });
});

it("normalizes false name-matching flags before persisting an exact save body", async () => {
  const remote = server();
  const outbox = createMetadataPolicyOutbox(remote.api);
  const value = input();
  if (value.definition.rules?.scene?.mappings) {
    value.definition.rules.scene.mappings.title = {
      jq: ".entity.title",
      performer_names: false,
      reference_names: false,
    };
  }
  const saved = await outbox.prepare(value);
  expect(JSON.parse(saved.body).definition.rules.scene.mappings.title).toEqual({
    jq: ".entity.title",
  });
  await outbox.deliver(collectionID);
  expect(await outbox.read(collectionID)).toBeNull();
});

it("rejects mismatched collection identities and nonadvancing history", async () => {
  const remote = server();
  remote.commit(input());
  const policy = await remote.api.policy(collectionID);
  remote.transport.mockResolvedValueOnce(
    Response.json({ ...policy, collection_uuid: rootID }),
  );
  await expect(remote.api.policy(collectionID)).rejects.toMatchObject({
    code: "policy_mismatch",
  });
  remote.transport.mockResolvedValueOnce(Response.json([policy]));
  await expect(remote.api.history(collectionID, 1)).rejects.toMatchObject({
    code: "history_mismatch",
  });
});
