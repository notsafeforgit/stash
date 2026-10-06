import type {
  DownloadStatus,
  DownloadTransfer,
} from "../../src/core/native-archive/download-api";
import { albumUUID } from "./source-albums";

export const downloadTime = "2026-10-06T12:00:00Z";
export function downloadTransfer(
  sequence = 100,
  attachment = albumUUID(100),
): DownloadTransfer {
  return {
    sequence,
    attachment_uuid: attachment,
    producer_uuid: albumUUID(4001),
    run_uuid: albumUUID(4002),
    fence: 2,
    transfer_sequence: sequence,
    capture_event_uuid: albumUUID(5000 + sequence),
    collection_uuid: albumUUID(4003),
    collection_revision: 3,
    collection_label: "Source account",
    root_uuid: albumUUID(4004),
    root_label: "Archive",
    state: "downloaded",
    reported_state: "downloaded",
    start_event_uuid: albumUUID(6000 + sequence),
    terminal_event_uuid: albumUUID(7000 + sequence),
    started_at: "2026-10-06T11:00:00Z",
    finished_at: "2026-10-06T11:05:00Z",
    first_recorded_at: "2026-10-06T11:05:01Z",
    last_recorded_at: "2026-10-06T11:05:02Z",
    file_event_uuid: albumUUID(8000 + sequence),
    verification_job_uuid: albumUUID(9000 + sequence),
    verification_state: "queued",
  };
}
export function emptyDownloadStatus(attachments: string[]): DownloadStatus {
  return {
    checked_at: downloadTime,
    attachments: attachments.map((attachment_uuid) => ({
      attachment_uuid,
      latest: null,
    })),
  };
}
