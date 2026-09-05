/**
 * Scene detail tab panel components.
 *
 * Each export is a self-contained panel that receives the scene and renders
 * its content. Tabs are assembled in the route component.
 */

import type React from "react";
import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { galleryLabel } from "src/lib/gallery-utils";
import type * as GQL from "src/core/generated-graphql";
import * as GQLM from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { Button } from "src/components/ui/button";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import {
  PlusIcon,
  Trash2Icon,
  MoreHorizontalIcon,
  FileXIcon,
  ImageOffIcon,
  LayersIcon,
  HistoryIcon,
  DropletIcon,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "src/components/ui/dialog";
import { DateInput } from "src/components/ui/date-input";
import { useMutation } from "@apollo/client/react";
import { SceneFileActionsMenu } from "src/components/detail/scene-file-actions";
import { MetaRow } from "src/components/detail/meta-row";
import { FingerprintMetaRows } from "src/components/detail/fingerprint-meta-rows";
import { FilterUrlLink } from "src/components/shared/filter-url-link";
import NavUtils from "src/utils/navigation";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";
import {
  CreatedUpdatedMetaRows,
  FileModTimeMetaRow,
} from "src/components/detail/timestamp-meta-rows";

// ── Shared helpers ─────────────────────────────────────────────────────────────

// Per-tab empty-state block. Same shape every tab uses, parameterised
// on the icon + title so each empty case (no files / galleries /
// groups / plays / o-history) reads with its own visual cue.
function TabEmptyState({
  icon,
  title,
}: {
  icon: React.ReactNode;
  title: string;
}) {
  return (
    <Empty className="border border-dashed border-border rounded-lg">
      <EmptyHeader>
        <EmptyMedia variant="icon">{icon}</EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
      </EmptyHeader>
    </Empty>
  );
}

type SceneData = NonNullable<GQL.FindSceneQuery["findScene"]>;

function Rating({ value }: { value: number | null | undefined }) {
  if (!value) return <span className="text-muted-foreground">—</span>;
  // rating100 → display as /100
  return <span>{value}</span>;
}

// ── Details tab ────────────────────────────────────────────────────────────────

export function SceneDetailsTab({ scene }: { scene: SceneData }) {
  const intl = useIntl();

  return (
    <dl className="grid m-0 p-0">
      {scene.title && (
        <MetaRow
          label={intl.formatMessage({ id: "title", defaultMessage: "Title" })}
        >
          {scene.title}
        </MetaRow>
      )}
      {scene.date && (
        <MetaRow
          label={intl.formatMessage({ id: "date", defaultMessage: "Date" })}
        >
          {scene.date}
        </MetaRow>
      )}
      {scene.code && (
        <MetaRow
          selectableText
          label={intl.formatMessage({
            id: "scene_code",
            defaultMessage: "Scene code",
          })}
        >
          {scene.code}
        </MetaRow>
      )}
      {scene.director && (
        <MetaRow
          label={intl.formatMessage({
            id: "director",
            defaultMessage: "Director",
          })}
        >
          <FilterUrlLink href={NavUtils.makeDirectorScenesUrl(scene.director)}>
            {scene.director}
          </FilterUrlLink>
        </MetaRow>
      )}
      {scene.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          <Rating value={scene.rating100} />
        </MetaRow>
      )}
      {scene.studio && (
        <MetaRow
          label={intl.formatMessage({ id: "studio", defaultMessage: "Studio" })}
        >
          <Link to="/studios/$studioId" params={{ studioId: scene.studio.id }}>
            {scene.studio.name}
          </Link>
        </MetaRow>
      )}
      {scene.performers.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "performers",
            defaultMessage: "Performers",
          })}
        >
          <div className="flex flex-col gap-1">
            {scene.performers.map((p) => (
              <Link
                key={p.id}
                to="/performers/$performerId"
                params={{ performerId: p.id }}
                className="text-primary no-underline hover:underline"
              >
                {p.disambiguation ? `${p.name} (${p.disambiguation})` : p.name}
              </Link>
            ))}
          </div>
        </MetaRow>
      )}
      {scene.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {scene.tags.map((t) => (
              <Badge
                key={t.id}
                variant="secondary"
                render={<Link to="/tags/$tagId" params={{ tagId: t.id }} />}
              >
                {t.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}
      {scene.urls.length > 0 && (
        <MetaRow
          selectableText
          label={intl.formatMessage({ id: "url", defaultMessage: "URL" })}
        >
          <div className="flex flex-col gap-1">
            {scene.urls.map((url) => (
              <a
                key={url}
                href={url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary no-underline hover:underline"
              >
                {url}
              </a>
            ))}
          </div>
        </MetaRow>
      )}
      {scene.details && (
        <MetaRow
          label={intl.formatMessage({
            id: "details",
            defaultMessage: "Details",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{scene.details}</p>
        </MetaRow>
      )}
      {scene.o_counter != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "o_counter",
            defaultMessage: "O-Counter",
          })}
        >
          {scene.o_counter}
        </MetaRow>
      )}
      {scene.play_count != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "play_count",
            defaultMessage: "Play count",
          })}
        >
          {scene.play_count}
        </MetaRow>
      )}
      {scene.organized && (
        <MetaRow
          label={intl.formatMessage({
            id: "organized",
            defaultMessage: "Organized",
          })}
        >
          ✓
        </MetaRow>
      )}
      <CreatedUpdatedMetaRows
        createdAt={scene.created_at}
        updatedAt={scene.updated_at}
      />
      <CustomFieldsRows values={scene.custom_fields} />
    </dl>
  );
}

// ── File Info tab ──────────────────────────────────────────────────────────────

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / 1024 ** i).toFixed(2)} ${units[i]}`;
}

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0)
    return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export function SceneFileInfoTab({ scene }: { scene: SceneData }) {
  const intl = useIntl();

  if (scene.files.length === 0) {
    return (
      <TabEmptyState
        icon={<FileXIcon />}
        title={intl.formatMessage({
          id: "no_files",
          defaultMessage: "No files",
        })}
      />
    );
  }

  // First file is the primary by API contract — only one file per scene can
  // be primary, and it always sorts to index 0.
  const ofMany = scene.files.length > 1;

  return (
    <div className="flex flex-col gap-4">
      {scene.files.map((file, i) => {
        const isPrimary = i === 0;
        return (
          <div key={file.id} className="border border-border rounded-lg p-3">
            {ofMany && (
              <div className="font-medium text-sm mb-2 flex items-center justify-between gap-2">
                <span className="flex items-center gap-2 min-w-0">
                  <span
                    className="truncate"
                    title={file.path}
                    data-selectable-text
                  >
                    {file.path.split("/").pop() || file.path}
                  </span>
                  {isPrimary && (
                    <Badge variant="secondary">
                      {intl.formatMessage({
                        id: "primary_file",
                        defaultMessage: "Primary",
                      })}
                    </Badge>
                  )}
                </span>
                {!isPrimary && (
                  <SceneFileActionsMenu
                    scene={scene}
                    fileId={file.id}
                    filePath={file.path}
                  />
                )}
              </div>
            )}
            <dl className="grid m-0 p-0">
              <MetaRow
                selectableText
                label={intl.formatMessage({
                  id: "path",
                  defaultMessage: "Path",
                })}
              >
                <span className="font-mono text-xs break-all">{file.path}</span>
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "size",
                  defaultMessage: "Size",
                })}
              >
                {formatBytes(file.size)}
              </MetaRow>
              <FileModTimeMetaRow modTime={file.mod_time} />
              <MetaRow
                label={intl.formatMessage({
                  id: "duration",
                  defaultMessage: "Duration",
                })}
              >
                {formatDuration(file.duration)}
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "dimensions",
                  defaultMessage: "Dimensions",
                })}
              >
                {file.width} × {file.height}
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "video_codec",
                  defaultMessage: "Video codec",
                })}
              >
                {file.video_codec}
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "audio_codec",
                  defaultMessage: "Audio codec",
                })}
              >
                {file.audio_codec}
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "frame_rate",
                  defaultMessage: "Frame rate",
                })}
              >
                {file.frame_rate.toFixed(2)} fps
              </MetaRow>
              <MetaRow
                label={intl.formatMessage({
                  id: "bitrate",
                  defaultMessage: "Bitrate",
                })}
              >
                {(file.bit_rate / 1000).toFixed(0)} kbps
              </MetaRow>
              <FingerprintMetaRows
                fingerprints={file.fingerprints}
                mode="scenes"
              />
            </dl>
          </div>
        );
      })}
    </div>
  );
}

// `SceneMarkersTab` lives in its own file (`scene-markers-tab.tsx`) — the
// tab grew an inline editor that pulls in mutations and a Sheet, large
// enough to warrant the split.

// ── Galleries tab ──────────────────────────────────────────────────────────────

export function SceneGalleriesTab({ scene }: { scene: SceneData }) {
  const intl = useIntl();

  if (scene.galleries.length === 0) {
    return (
      <TabEmptyState
        icon={<ImageOffIcon />}
        title={intl.formatMessage({
          id: "no_galleries",
          defaultMessage: "No galleries",
        })}
      />
    );
  }

  return (
    <ul className="flex flex-col gap-1.5 list-none m-0 p-0">
      {scene.galleries.map((gallery) => (
        <li key={gallery.id} className="flex items-center gap-2 text-sm">
          <Link to="/galleries/$galleryId" params={{ galleryId: gallery.id }}>
            {galleryLabel(gallery)}
          </Link>
          {gallery.image_count > 0 && (
            <span className="text-muted-foreground text-xs ml-2">
              {gallery.image_count}{" "}
              {intl.formatMessage({ id: "images", defaultMessage: "images" })}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

// ── Groups tab ─────────────────────────────────────────────────────────────────

export function SceneGroupsTab({ scene }: { scene: SceneData }) {
  const intl = useIntl();

  if (scene.groups.length === 0) {
    return (
      <TabEmptyState
        icon={<LayersIcon />}
        title={intl.formatMessage({
          id: "no_groups",
          defaultMessage: "No groups",
        })}
      />
    );
  }

  return (
    <ul className="flex flex-col gap-1.5 list-none m-0 p-0">
      {scene.groups.map(({ group, scene_index }) => (
        <li key={group.id} className="flex items-center gap-2 text-sm">
          {group.front_image_path && (
            <img
              src={group.front_image_path}
              alt={group.name}
              className="rounded h-12 object-cover w-8 shrink-0"
              loading="lazy"
            />
          )}
          <div>
            <Link to="/groups/$groupId" params={{ groupId: group.id }}>
              {group.name}
            </Link>
            {scene_index != null && (
              <span className="text-muted-foreground text-xs ml-2">
                #{scene_index}
              </span>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}

// ── History tab ────────────────────────────────────────────────────────────────

const fmt = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "short",
});

/** Convert a "YYYY-MM-DD HH:MM" string to an ISO timestamp string for mutations. */
function dateInputToISO(value: string): string {
  const trimmed = value.trim();
  // "YYYY-MM-DD HH:MM" → "YYYY-MM-DDTHH:MM:00"
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(trimmed)) {
    return trimmed.replace(" ", "T") + ":00";
  }
  // "YYYY-MM-DD" → "YYYY-MM-DDT00:00:00"
  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) {
    return trimmed + "T00:00:00";
  }
  return new Date().toISOString();
}

/** Add a custom-date dialog that calls an async submit handler. */
function AddCustomDateDialog({
  open,
  onOpenChange,
  title,
  description,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  description: string;
  onSubmit: (isoTimestamp: string) => Promise<void>;
}) {
  const intl = useIntl();
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);

  async function handleSubmit() {
    if (!value) return;
    setBusy(true);
    try {
      await onSubmit(dateInputToISO(value));
      onOpenChange(false);
      setValue("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={busy ? () => {} : onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DateInput value={value} onValueChange={setValue} isTime />
        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={!value || busy}>
            {intl.formatMessage({ id: "actions.add", defaultMessage: "Add" })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Confirmation dialog for destructive clear operations. */
function ConfirmClearDialog({
  open,
  onOpenChange,
  title,
  message,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  title: string;
  message: string;
  onConfirm: () => Promise<void>;
}) {
  const intl = useIntl();
  const [busy, setBusy] = useState(false);

  async function handleConfirm() {
    setBusy(true);
    try {
      await onConfirm();
      onOpenChange(false);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={busy ? () => {} : onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{message}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            onClick={handleConfirm}
            disabled={busy}
          >
            {intl.formatMessage({
              id: "actions.confirm",
              defaultMessage: "Confirm",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function SceneHistoryTab({ scene }: { scene: SceneData }) {
  const intl = useIntl();

  const playEntries = useMemo(
    () => [...scene.play_history].sort((a, b) => b.localeCompare(a)),
    [scene.play_history],
  );
  const oEntries = useMemo(
    () => [...scene.o_history].sort((a, b) => b.localeCompare(a)),
    [scene.o_history],
  );

  // ── Mutations ──────────────────────────────────────────────────────────────

  function playHistoryFields(result: GQLM.HistoryMutationResult) {
    return {
      play_history: () => result.history,
      play_count: () => result.count,
    };
  }

  function oHistoryFields(result: GQLM.HistoryMutationResult) {
    return {
      o_history: () => result.history,
      o_counter: () => result.count,
    };
  }

  const cacheId = `Scene:${scene.id}`;

  const [addPlay] = useMutation(GQLM.SceneAddPlayDocument, {
    update(cache, { data }) {
      if (!data?.sceneAddPlay) return;
      cache.modify({
        id: cacheId,
        fields: playHistoryFields(data.sceneAddPlay),
      });
    },
  });
  const [deletePlay] = useMutation(GQLM.SceneDeletePlayDocument, {
    update(cache, { data }) {
      if (!data?.sceneDeletePlay) return;
      cache.modify({
        id: cacheId,
        fields: playHistoryFields(data.sceneDeletePlay),
      });
    },
  });
  const [resetPlayCount] = useMutation(GQLM.SceneResetPlayCountDocument, {
    update(cache, { data }) {
      if (data?.sceneResetPlayCount == null) return;
      cache.modify({
        id: cacheId,
        fields: {
          play_history: () => [],
          play_count: () => data.sceneResetPlayCount,
        },
      });
    },
  });
  const [resetActivity] = useMutation(GQLM.SceneResetActivityDocument, {
    update(cache) {
      cache.modify({
        id: cacheId,
        fields: {
          resume_time: () => 0,
          play_duration: () => 0,
        },
      });
    },
  });

  const [addO] = useMutation(GQLM.SceneAddODocument, {
    update(cache, { data }) {
      if (!data?.sceneAddO) return;
      cache.modify({ id: cacheId, fields: oHistoryFields(data.sceneAddO) });
    },
  });
  const [deleteO] = useMutation(GQLM.SceneDeleteODocument, {
    update(cache, { data }) {
      if (!data?.sceneDeleteO) return;
      cache.modify({ id: cacheId, fields: oHistoryFields(data.sceneDeleteO) });
    },
  });
  const [resetO] = useMutation(GQLM.SceneResetODocument, {
    update(cache, { data }) {
      if (data?.sceneResetO == null) return;
      cache.modify({
        id: cacheId,
        fields: {
          o_history: () => [],
          o_counter: () => data.sceneResetO,
        },
      });
    },
  });

  // ── Dialog state ───────────────────────────────────────────────────────────

  const [playCustomOpen, setPlayCustomOpen] = useState(false);
  const [clearPlaysOpen, setClearPlaysOpen] = useState(false);
  const [oCustomOpen, setOCustomOpen] = useState(false);
  const [clearOOpen, setClearOOpen] = useState(false);

  // ── Render ─────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-col gap-6">
      {/* Play history section */}
      <section>
        <div className="flex items-center gap-1.5 mb-2">
          <h3 className="text-[0.9375rem] font-semibold m-0 flex-1">
            {intl.formatMessage({
              id: "play_history",
              defaultMessage: "Play history",
            })}
            <span className="text-muted-foreground text-xs ml-2">
              ({playEntries.length})
            </span>
          </h3>
          <Button
            variant="ghost"
            size="icon-sm"
            title={intl.formatMessage({
              id: "actions.add_play_now",
              defaultMessage: "Add play (now)",
            })}
            onClick={() => addPlay({ variables: { id: scene.id, times: [] } })}
          >
            <PlusIcon size={15} />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" />}
            >
              <MoreHorizontalIcon size={15} />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setPlayCustomOpen(true)}>
                {intl.formatMessage({
                  id: "actions.add_play_custom_date",
                  defaultMessage: "Add with custom date…",
                })}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() =>
                  resetActivity({
                    variables: {
                      id: scene.id,
                      reset_resume: true,
                      reset_duration: false,
                    },
                  })
                }
              >
                {intl.formatMessage({
                  id: "actions.reset_resume_time",
                  defaultMessage: "Reset resume time",
                })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() =>
                  resetActivity({
                    variables: {
                      id: scene.id,
                      reset_resume: false,
                      reset_duration: true,
                    },
                  })
                }
              >
                {intl.formatMessage({
                  id: "actions.reset_play_duration",
                  defaultMessage: "Reset play duration",
                })}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setClearPlaysOpen(true)}
              >
                {intl.formatMessage({
                  id: "actions.clear_play_history",
                  defaultMessage: "Clear all plays…",
                })}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {playEntries.length === 0 ? (
          <TabEmptyState
            icon={<HistoryIcon />}
            title={intl.formatMessage({
              id: "no_plays",
              defaultMessage: "No plays recorded",
            })}
          />
        ) : (
          <ul className="flex flex-col gap-0.5 list-none m-0 p-0 max-h-64 overflow-y-auto">
            {playEntries.map((ts) => (
              <li key={ts} className="flex items-center gap-1 text-sm">
                <span className="flex-1">{fmt.format(new Date(ts))}</span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-destructive hover:text-destructive"
                  title={intl.formatMessage({
                    id: "actions.delete",
                    defaultMessage: "Delete",
                  })}
                  onClick={() =>
                    deletePlay({ variables: { id: scene.id, times: [ts] } })
                  }
                >
                  <Trash2Icon size={13} />
                </Button>
              </li>
            ))}
          </ul>
        )}

        {(scene.last_played_at || scene.play_duration != null) && (
          <div className="mt-2 flex flex-col gap-0.5">
            {scene.last_played_at && (
              <p className="text-muted-foreground text-xs m-0">
                {intl.formatMessage({
                  id: "last_played_at",
                  defaultMessage: "Last played",
                })}
                {": "}
                {fmt.format(new Date(scene.last_played_at))}
              </p>
            )}
            {scene.play_duration != null && (
              <p className="text-muted-foreground text-xs m-0">
                {intl.formatMessage({
                  id: "total_play_duration",
                  defaultMessage: "Total play duration",
                })}
                {": "}
                {formatDuration(scene.play_duration)}
              </p>
            )}
          </div>
        )}
      </section>

      {/* O history section */}
      <section>
        <div className="flex items-center gap-1.5 mb-2">
          <h3 className="text-[0.9375rem] font-semibold m-0 flex-1">
            {intl.formatMessage({
              id: "o_history",
              defaultMessage: "O history",
            })}
            <span className="text-muted-foreground text-xs ml-2">
              ({oEntries.length})
            </span>
          </h3>
          <Button
            variant="ghost"
            size="icon-sm"
            title={intl.formatMessage({
              id: "actions.add_o_now",
              defaultMessage: "Add O (now)",
            })}
            onClick={() => addO({ variables: { id: scene.id, times: [] } })}
          >
            <PlusIcon size={15} />
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon-sm" />}
            >
              <MoreHorizontalIcon size={15} />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={() => setOCustomOpen(true)}>
                {intl.formatMessage({
                  id: "actions.add_o_custom_date",
                  defaultMessage: "Add with custom date…",
                })}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setClearOOpen(true)}
              >
                {intl.formatMessage({
                  id: "actions.clear_o_history",
                  defaultMessage: "Clear all O history…",
                })}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {oEntries.length === 0 ? (
          <TabEmptyState
            icon={<DropletIcon />}
            title={intl.formatMessage({
              id: "no_o_history",
              defaultMessage: "No O history recorded",
            })}
          />
        ) : (
          <ul className="flex flex-col gap-0.5 list-none m-0 p-0 max-h-64 overflow-y-auto">
            {oEntries.map((ts) => (
              <li key={ts} className="flex items-center gap-1 text-sm">
                <span className="flex-1">{fmt.format(new Date(ts))}</span>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-destructive hover:text-destructive"
                  title={intl.formatMessage({
                    id: "actions.delete",
                    defaultMessage: "Delete",
                  })}
                  onClick={() =>
                    deleteO({ variables: { id: scene.id, times: [ts] } })
                  }
                >
                  <Trash2Icon size={13} />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {/* Dialogs */}
      <AddCustomDateDialog
        open={playCustomOpen}
        onOpenChange={setPlayCustomOpen}
        title={intl.formatMessage({
          id: "dialogs.add_play_custom_date_title",
          defaultMessage: "Add play entry",
        })}
        description={intl.formatMessage({
          id: "dialogs.add_play_custom_date_desc",
          defaultMessage: "Record a play at a specific date and time.",
        })}
        onSubmit={(ts) =>
          addPlay({ variables: { id: scene.id, times: [ts] } }).then(() => {})
        }
      />
      <AddCustomDateDialog
        open={oCustomOpen}
        onOpenChange={setOCustomOpen}
        title={intl.formatMessage({
          id: "dialogs.add_o_custom_date_title",
          defaultMessage: "Add O entry",
        })}
        description={intl.formatMessage({
          id: "dialogs.add_o_custom_date_desc",
          defaultMessage: "Record an O at a specific date and time.",
        })}
        onSubmit={(ts) =>
          addO({ variables: { id: scene.id, times: [ts] } }).then(() => {})
        }
      />
      <ConfirmClearDialog
        open={clearPlaysOpen}
        onOpenChange={setClearPlaysOpen}
        title={intl.formatMessage({
          id: "dialogs.clear_play_history_title",
          defaultMessage: "Clear play history?",
        })}
        message={intl.formatMessage({
          id: "dialogs.confirm_clear_play_history",
          defaultMessage:
            "This will permanently delete all play history for this scene.",
        })}
        onConfirm={() =>
          resetPlayCount({ variables: { id: scene.id } }).then(() => {})
        }
      />
      <ConfirmClearDialog
        open={clearOOpen}
        onOpenChange={setClearOOpen}
        title={intl.formatMessage({
          id: "dialogs.clear_o_history_title",
          defaultMessage: "Clear O history?",
        })}
        message={intl.formatMessage({
          id: "dialogs.confirm_clear_o_history",
          defaultMessage:
            "This will permanently delete all O history for this scene.",
        })}
        onConfirm={() => resetO({ variables: { id: scene.id } }).then(() => {})}
      />
    </div>
  );
}
