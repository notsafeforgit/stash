const PHASH_DISTANCE_THRESHOLD = 8;
const NIBBLE_POPCOUNT = [0, 1, 1, 2, 1, 2, 2, 3, 1, 2, 2, 3, 2, 3, 3, 4];

interface LocalFingerprint {
  type: string;
  value: string;
}

interface LocalFile {
  fingerprints: readonly LocalFingerprint[];
}

interface RemoteFingerprint {
  algorithm: string;
  hash: string;
  submissions: number;
}

export interface PhashMatch {
  hash: string;
  distance: number;
}

export interface SceneFingerprintMatches {
  oshash: RemoteFingerprint[];
  phash: PhashMatch[];
}

export function fingerprintDistance(a: string, b: string): number {
  if (!a || a.length !== b.length) return Number.POSITIVE_INFINITY;

  let distance = 0;
  for (let index = 0; index < a.length; index += 1) {
    const left = Number.parseInt(a[index], 16);
    const right = Number.parseInt(b[index], 16);
    if (Number.isNaN(left) || Number.isNaN(right)) {
      return Number.POSITIVE_INFINITY;
    }
    distance += NIBBLE_POPCOUNT[left ^ right];
  }
  return distance;
}

export function findSceneFingerprintMatches(
  remoteFingerprints: readonly RemoteFingerprint[],
  localFiles: readonly LocalFile[],
): SceneFingerprintMatches {
  const localFingerprints = localFiles.flatMap((file) => file.fingerprints);
  const localOshashes = new Set(
    localFingerprints
      .filter((fingerprint) => fingerprint.type === "oshash")
      .map((fingerprint) => fingerprint.value),
  );
  const localPhashes = localFingerprints.filter(
    (fingerprint) => fingerprint.type === "phash",
  );

  const oshash = remoteFingerprints.filter(
    (fingerprint) =>
      fingerprint.algorithm === "OSHASH" && localOshashes.has(fingerprint.hash),
  );

  const bestPhashDistance = new Map<string, number>();
  for (const remote of remoteFingerprints) {
    if (remote.algorithm !== "PHASH") continue;

    let best = Number.POSITIVE_INFINITY;
    for (const local of localPhashes) {
      best = Math.min(best, fingerprintDistance(remote.hash, local.value));
    }
    if (best <= PHASH_DISTANCE_THRESHOLD) {
      const current = bestPhashDistance.get(remote.hash);
      if (current === undefined || best < current) {
        bestPhashDistance.set(remote.hash, best);
      }
    }
  }

  const phash = [...bestPhashDistance.entries()]
    .map(([hash, distance]) => ({ hash, distance }))
    .sort((left, right) => left.distance - right.distance);

  return { oshash, phash };
}
