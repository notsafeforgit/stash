import type * as GQL from "src/core/generated-graphql";

export type PerformerCollision =
  | "stash_mismatch"
  | "birthdate"
  | "country"
  | "ethnicity"
  | "gender";

type RemotePerformer = Pick<
  GQL.ScrapedScenePerformerDataFragment,
  "remote_site_id" | "birthdate" | "country" | "ethnicity" | "gender"
>;

interface LocalPerformer {
  stash_ids: Array<{ endpoint: string; stash_id: string }>;
  birthdate?: GQL.PerformerDataFragment["birthdate"];
  country?: GQL.PerformerDataFragment["country"];
  ethnicity?: GQL.PerformerDataFragment["ethnicity"];
  gender?: GQL.PerformerDataFragment["gender"];
}

function normalizeValue(value: unknown): string {
  const text = String(value ?? "").trim();
  if (!text) return "";

  if (/^[-+]?(?:\d+\.?\d*|\.\d+)$/.test(text)) {
    const numeric = Number(text);
    if (!Number.isNaN(numeric)) return String(numeric);
  }

  return text.toLowerCase();
}

function valuesCollide(remoteValue: unknown, localValue: unknown): boolean {
  const remoteText = String(remoteValue ?? "").trim();
  if (!remoteText) return false;
  return normalizeValue(remoteText) !== normalizeValue(localValue);
}

export function getPerformerCollisions(
  remote: RemotePerformer,
  local: LocalPerformer,
  endpoint?: string,
): PerformerCollision[] {
  const collisions: PerformerCollision[] = [];

  if (
    endpoint &&
    remote.remote_site_id &&
    local.stash_ids.some(
      (id) => id.endpoint === endpoint && id.stash_id !== remote.remote_site_id,
    )
  ) {
    collisions.push("stash_mismatch");
  }
  if (valuesCollide(remote.birthdate, local.birthdate)) {
    collisions.push("birthdate");
  }
  if (valuesCollide(remote.country, local.country)) {
    collisions.push("country");
  }
  if (valuesCollide(remote.gender, local.gender)) {
    collisions.push("gender");
  }
  if (valuesCollide(remote.ethnicity, local.ethnicity)) {
    collisions.push("ethnicity");
  }

  return collisions;
}
