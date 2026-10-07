import type {
  ActivityAttempt,
  JobActivity,
  JobActivityDetail,
  RunActivity,
  RunActivityDetail,
} from "../../src/core/native-archive/activity-schema";

export const activityIds = {
  job: "11111111-1111-4111-8111-111111111111",
  other: "22222222-2222-4222-8222-222222222222",
  run: "33333333-3333-4333-8333-333333333333",
  collection: "44444444-4444-4444-8444-444444444444",
};
const clock = "2026-10-06T12:00:00Z";
const common = {
  sequence: 10,
  revision: 1,
  attempt_count: 0,
  available_at: clock,
  lease_until: null,
  error_code: "",
  created_at: clock,
  updated_at: clock,
};
export function activityJob(): JobActivity {
  return {
    ...common,
    uuid: activityIds.job,
    kind: "media.verify",
    state: "queued",
    max_attempts: 3,
  };
}
export function activityRun(): RunActivity {
  return {
    ...common,
    uuid: activityIds.run,
    collection_uuid: activityIds.collection,
    collection_revision: 1,
    collection_label: "Example source",
    target_url: "https://www.reddit.com/user/example/submitted/",
    operation: "enrich",
    state: "queued",
    failures: 0,
    pending_windows: 1,
    completed_windows: 0,
  };
}
export function activityJobDetail(): JobActivityDetail {
  return {
    summary: activityJob(),
    subjects: [
      {
        kind: "collection",
        uuid: activityIds.collection,
        requested_uuid: activityIds.collection,
        title: "Example source",
        title_truncated: false,
        state: "active",
        revision: 1,
        local_id: null,
      },
    ],
    context_available: true,
    relative_path: "Example clip.mp4",
    manual_request_uuid: "",
    merge_request_uuid: "",
  };
}
export function activityRunDetail(): RunActivityDetail {
  return {
    summary: activityRun(),
    pending: [{ since: null, until: clock }],
    completed: [],
    window: null,
    progress: { items_seen: 0, files_completed: 0, cursor: "" },
    root_uuid: null,
    path_prefix: "",
    recovery: null,
  };
}
export function activityAttempt(): ActivityAttempt {
  return {
    number: 1,
    started_at: clock,
    ended_at: "2026-10-06T12:01:00Z",
    outcome: "retry",
    error_code: "temporary_failure",
  };
}
