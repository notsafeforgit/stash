import { useId, type ReactNode, useEffect, useMemo, useState } from "react";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { z } from "zod";
import { useMutation, useQuery } from "@apollo/client/react";
import { FormattedMessage, FormattedNumber, useIntl } from "react-intl";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  CalendarArrowDown,
  CalendarArrowUp,
  CheckSquare,
  Clapperboard,
  FileVideo,
  Filter,
  GitMerge,
  Pencil,
  Ruler,
  Trash2,
  XSquare,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { objectTitle } from "src/core/files";
import { ListFilterModel } from "src/models/list-filter/filter";
import { ToolFilterSidebar } from "src/components/filters/tool-filter-sidebar";
import {
  MediaColorBadge,
  compareMediaColor,
  mediaColorValueKey,
} from "src/components/shared/media-color-badge";
import {
  DEFAULT_DUPLICATE_FILTER_SCOPE,
  DUPLICATE_FILTER_SCOPES,
  DuplicateFilterScopeToggle,
  type DuplicateFilterScope,
} from "src/components/filters/duplicate-filter-scope-toggle";
import { SceneLightbox, type SceneSlide } from "src/components/lightbox";
import {
  fileSize,
  fileSizeFractionalDigits,
  formatFileSizeUnit,
} from "src/utils/file";
import { secondsToTimestamp } from "src/utils/duration";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "src/components/ui/card";
import { Checkbox } from "src/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import { Label } from "src/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "src/components/ui/table";
import { Badge } from "src/components/ui/badge";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { Skeleton } from "src/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import {
  DeleteDialog,
  DeleteFilesList,
  type DeleteOptions,
} from "src/components/detail/delete-dialog";
import { SceneBulkEditSheet } from "src/components/detail/scene-bulk-edit-sheet";
import { SceneMergeDialog } from "src/components/detail/scene-merge-dialog";
import { useToast } from "src/hooks/toast";
import { useDocumentTitle } from "src/hooks/title";

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 20;
const DEFAULT_DISTANCE = 0;
const DEFAULT_DURATION_DIFF = 1;

const PAGE_SIZES = [
  10, 20, 30, 40, 50, 100, 150, 200, 250, 500, 750, 1000, 1250, 1500,
];
const TABLE_SKELETON_ROWS = 12;
const SORT_VALUE_TINT_CLASSES = [
  "bg-sky-500/10 dark:bg-sky-400/15",
  "bg-emerald-500/10 dark:bg-emerald-400/15",
  "bg-amber-500/10 dark:bg-amber-400/15",
  "bg-rose-500/10 dark:bg-rose-400/15",
  "bg-violet-500/10 dark:bg-violet-400/15",
  "bg-cyan-500/10 dark:bg-cyan-400/15",
  "bg-lime-500/10 dark:bg-lime-400/15",
  "bg-orange-500/10 dark:bg-orange-400/15",
  "bg-fuchsia-500/10 dark:bg-fuchsia-400/15",
  "bg-teal-500/10 dark:bg-teal-400/15",
];

const SCENE_ACCURACY_OPTIONS = [
  { value: 0, id: "dupe_check.options.exact", defaultMessage: "Exact" },
  { value: 4, id: "dupe_check.options.high", defaultMessage: "High" },
  { value: 8, id: "dupe_check.options.medium", defaultMessage: "Medium" },
  { value: 10, id: "dupe_check.options.low", defaultMessage: "Low" },
] as const;

const SCENE_DURATION_OPTIONS = [
  { value: -1, id: "dupe_check.duration_options.any", defaultMessage: "Any" },
  {
    value: 0,
    id: "dupe_check.duration_options.equal",
    defaultMessage: "Equal",
  },
  { value: 1 },
  { value: 5 },
  { value: 10 },
] as const;

const searchSchema = z.object({
  page: z.coerce.number().int().positive().optional(),
  size: z.coerce.number().int().positive().optional(),
  distance: z.coerce.number().int().min(0).optional(),
  durationDiff: z.coerce.number().min(-1).optional(),
  fa: z.string().optional(),
  filterScope: z.enum(DUPLICATE_FILTER_SCOPES).optional().catch(undefined),
});

type SceneDuplicate =
  GQL.FindDuplicateScenesQuery["findDuplicateSceneGroups"]["groups"][number][number];
type SceneGroup = SceneDuplicate[];
type SceneDuplicateFile = SceneDuplicate["files"][number];
type SceneSortColumn =
  | "details"
  | "metadata"
  | "duration"
  | "filesize"
  | "resolution"
  | "bitrate"
  | "color"
  | "videoCodec"
  | "audioCodec";
type SortDirection = "asc" | "desc";
type SceneSortState = {
  column: SceneSortColumn;
  direction: SortDirection;
};

function MetadataBadge({
  children,
  items,
}: {
  children: ReactNode;
  items: string[];
}) {
  const tooltipItems = items.filter(Boolean);
  if (tooltipItems.length === 0) {
    return <Badge variant="secondary">{children}</Badge>;
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge variant="secondary" tabIndex={0} className="cursor-help">
            {children}
          </Badge>
        }
      />
      <TooltipContent className="max-w-sm">
        <div className="grid gap-1">
          {tooltipItems.map((item, index) => (
            <span key={`${item}-${index}`}>{item}</span>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function nextSceneSort(
  current: SceneSortState | undefined,
  column: SceneSortColumn,
): SceneSortState {
  return {
    column,
    direction:
      current?.column === column && current.direction === "asc"
        ? "desc"
        : "asc",
  };
}

function compareText(a: string, b: string, locale: string): number {
  return a.localeCompare(b, locale, {
    numeric: true,
    sensitivity: "base",
  });
}

function sceneMetadataScore(scene: SceneDuplicate): number {
  return (
    scene.tags.length +
    scene.performers.length +
    scene.groups.length +
    scene.scene_markers.length +
    scene.galleries.length +
    (scene.o_counter && scene.o_counter > 0 ? 1 : 0) +
    (scene.files.length > 1 ? scene.files.length : 0) +
    (scene.organized ? 1 : 0)
  );
}

function compareScenes(
  a: SceneDuplicate,
  b: SceneDuplicate,
  column: SceneSortColumn,
  locale: string,
): number {
  const aFile = primaryFile(a);
  const bFile = primaryFile(b);

  switch (column) {
    case "details":
      return compareText(
        `${objectTitle(a)} ${aFile?.path ?? ""}`,
        `${objectTitle(b)} ${bFile?.path ?? ""}`,
        locale,
      );
    case "metadata":
      return sceneMetadataScore(a) - sceneMetadataScore(b);
    case "duration":
      return (aFile?.duration ?? 0) - (bFile?.duration ?? 0);
    case "filesize":
      return sceneFileSize(a) - sceneFileSize(b);
    case "resolution":
      return sceneResolution(a) - sceneResolution(b);
    case "bitrate":
      return (aFile?.bit_rate ?? 0) - (bFile?.bit_rate ?? 0);
    case "color":
      return compareMediaColor(aFile, bFile, locale);
    case "videoCodec":
      return compareText(
        aFile?.video_codec ?? "",
        bFile?.video_codec ?? "",
        locale,
      );
    case "audioCodec":
      return compareText(
        aFile?.audio_codec ?? "",
        bFile?.audio_codec ?? "",
        locale,
      );
  }
}

function stableHash(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i++) {
    hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
  }
  return hash;
}

function sceneSortValueKey(
  scene: SceneDuplicate,
  column: SceneSortColumn,
): string {
  const file = primaryFile(scene);

  switch (column) {
    case "details":
      return `${objectTitle(scene).trim().toLocaleLowerCase()}\u0000${(file?.path ?? "").trim().toLocaleLowerCase()}`;
    case "metadata":
      return String(sceneMetadataScore(scene));
    case "duration":
      return String(file?.duration ?? 0);
    case "filesize":
      return String(sceneFileSize(scene));
    case "resolution":
      return String(sceneResolution(scene));
    case "bitrate":
      return String(file?.bit_rate ?? 0);
    case "color":
      return mediaColorValueKey(file);
    case "videoCodec":
      return (file?.video_codec ?? "").trim().toLocaleLowerCase();
    case "audioCodec":
      return (file?.audio_codec ?? "").trim().toLocaleLowerCase();
  }
}

function sceneSortTintClasses(
  groups: SceneGroup[],
  column: SceneSortColumn,
): Map<string, string> {
  const tintBySceneId = new Map<string, string>();

  for (const group of groups) {
    const counts = new Map<string, number>();
    for (const scene of group) {
      const key = sceneSortValueKey(scene, column);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }

    if (![...counts.values()].some((count) => count > 1)) continue;

    // Hash only varies the palette offset between groups; within a group each
    // distinct value takes the next palette entry, so two different values
    // can't share a tint until a group exceeds the palette size.
    const groupOffset = stableHash(
      `${group.map((scene) => scene.id).join(":")}:${column}`,
    );
    const tintByValue = new Map<string, string>();

    for (const scene of group) {
      const key = sceneSortValueKey(scene, column);
      let tint = tintByValue.get(key);
      if (!tint) {
        tint =
          SORT_VALUE_TINT_CLASSES[
            (groupOffset + tintByValue.size) % SORT_VALUE_TINT_CLASSES.length
          ];
        tintByValue.set(key, tint);
      }
      tintBySceneId.set(scene.id, tint);
    }
  }

  return tintBySceneId;
}

function formatBytes(bytes: number | undefined): string {
  const size = fileSize(bytes ?? 0);
  return `${size.size.toFixed(fileSizeFractionalDigits(size.unit))} ${formatFileSizeUnit(size.unit)}`;
}

function primaryFile(scene: SceneDuplicate): SceneDuplicateFile | undefined {
  return scene.files[0];
}

function sceneFileSize(scene: SceneDuplicate): number {
  return scene.files.reduce((max, file) => Math.max(max, file.size ?? 0), 0);
}

function sceneResolution(scene: SceneDuplicate): number {
  return scene.files.reduce(
    (max, file) => Math.max(max, (file.width ?? 0) * (file.height ?? 0)),
    0,
  );
}

function sceneGroupSize(group: SceneGroup): number {
  return group.reduce(
    (total, scene) =>
      total +
      scene.files.reduce((fileTotal, file) => fileTotal + (file.size ?? 0), 0),
    0,
  );
}

function newestOrOldestScene(
  group: SceneGroup,
  oldest: boolean,
): SceneDuplicate | undefined {
  let selected: SceneDuplicate | undefined;
  let selectedTime: number | undefined;

  for (const scene of group) {
    for (const file of scene.files) {
      const time = new Date(file.mod_time).getTime();
      if (!Number.isFinite(time)) continue;
      if (
        selectedTime === undefined ||
        (oldest ? time < selectedTime : time > selectedTime)
      ) {
        selected = scene;
        selectedTime = time;
      }
    }
  }

  return selected;
}

function sameResolution(group: SceneGroup): boolean {
  return new Set(group.map(sceneResolution)).size === 1;
}

function sameCodec(group: SceneGroup): boolean {
  return (
    new Set(
      group.map((scene) => {
        const file = primaryFile(scene);
        return `${file?.video_codec ?? ""}:${file?.audio_codec ?? ""}`;
      }),
    ).size === 1
  );
}

function pageCount(total: number, pageSize: number): number {
  return Math.max(1, Math.ceil(total / pageSize));
}

function duplicateFilterMode(
  scope: DuplicateFilterScope,
): GQL.DuplicateFilterMode {
  switch (scope) {
    case "any":
      return GQL.DuplicateFilterMode.Any;
    case "all":
      return GQL.DuplicateFilterMode.All;
  }
}

function accuracyLabel(
  value: number,
  formatMessage: ReturnType<typeof useIntl>["formatMessage"],
): string {
  const option = SCENE_ACCURACY_OPTIONS.find((item) => item.value === value);
  if (!option) return String(value);

  return formatMessage({
    id: option.id,
    defaultMessage: option.defaultMessage,
  });
}

function durationLabel(
  value: number,
  formatMessage: ReturnType<typeof useIntl>["formatMessage"],
): string {
  const option = SCENE_DURATION_OPTIONS.find((item) => item.value === value);
  if (!option) return String(value);

  if ("id" in option) {
    return formatMessage({
      id: option.id,
      defaultMessage: option.defaultMessage,
    });
  }

  return formatMessage(
    {
      id: "seconds_count",
      defaultMessage: "{count, plural, one {# second} other {# seconds}}",
    },
    { count: option.value },
  );
}

function SceneDuplicateCheckerPage() {
  const intl = useIntl();
  const controlId = useId();
  useDocumentTitle(
    intl.formatMessage({
      id: "config.tools.scene_duplicate_checker",
      defaultMessage: "Scene duplicate checker",
    }),
  );
  const navigate = useNavigate({ from: Route.fullPath });
  const toast = useToast();
  const search = Route.useSearch();

  const currentPage = search.page ?? DEFAULT_PAGE;
  const pageSize = search.size ?? DEFAULT_PAGE_SIZE;
  const hashDistance = search.distance ?? DEFAULT_DISTANCE;
  const durationDiff = search.durationDiff ?? DEFAULT_DURATION_DIFF;
  const filterScope = search.filterScope ?? DEFAULT_DUPLICATE_FILTER_SCOPE;

  const [checkedScenes, setCheckedScenes] = useState<Record<string, boolean>>(
    {},
  );
  const [safeSelect, setSafeSelect] = useState(true);
  const [bulkEditOpen, setBulkEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTargetScenes, setDeleteTargetScenes] = useState<
    SceneDuplicate[]
  >([]);
  const [mergeGroup, setMergeGroup] = useState<SceneGroup | null>(null);
  const [filterOpen, setFilterOpen] = useState(() => Boolean(search.fa));
  const [lightbox, setLightbox] = useState<{
    slides: SceneSlide[];
    index: number;
  } | null>(null);

  const [filterModel, setFilterModel] = useState(() => {
    const model = new ListFilterModel(GQL.FilterMode.Scenes);
    if (search.fa) model.configureFromDecodedParams({ fa: search.fa });
    return model;
  });

  function setFilter(next: ListFilterModel) {
    setFilterModel(next);
    setCheckedScenes({});
    void navigate({
      search: (prev) => ({
        ...prev,
        page: undefined,
        fa: next.getEncodedParams().fa ?? undefined,
      }),
      replace: true,
    });
  }

  const filterVariables = useMemo(
    () => ({
      scene_filter_ast: filterModel.makeFilterAST(),
    }),
    [filterModel],
  );
  const hasFilter = filterModel.count() > 0;
  const shouldFilterDuplicateQuery = hasFilter;
  const duplicateVariables = useMemo(
    () => ({
      distance: hashDistance,
      duration_diff: durationDiff,
      filter: {
        page: currentPage,
        per_page: pageSize,
      },
    }),
    [currentPage, durationDiff, hashDistance, pageSize],
  );
  const filteredDuplicateVariables = useMemo(
    () => ({
      distance: hashDistance,
      duration_diff: durationDiff,
      filter: {
        page: currentPage,
        per_page: pageSize,
      },
      filter_mode: duplicateFilterMode(filterScope),
      ...filterVariables,
    }),
    [
      currentPage,
      durationDiff,
      filterScope,
      filterVariables,
      hashDistance,
      pageSize,
    ],
  );

  // cache-first: revisiting the page within a session reuses the previous
  // result instead of recomputing all duplicates; the Refresh button
  // refetches explicitly.
  const unfilteredDuplicateQuery = useQuery(GQL.FindDuplicateScenesDocument, {
    variables: duplicateVariables,
    fetchPolicy: "cache-first",
    notifyOnNetworkStatusChange: true,
    skip: shouldFilterDuplicateQuery,
  });
  const filteredDuplicateQuery = useQuery(
    GQL.FindDuplicateScenesFilteredDocument,
    {
      variables: filteredDuplicateVariables,
      fetchPolicy: "cache-first",
      notifyOnNetworkStatusChange: true,
      skip: !shouldFilterDuplicateQuery,
    },
  );
  const duplicateResult = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.data?.findDuplicateSceneGroups
    : unfilteredDuplicateQuery.data?.findDuplicateSceneGroups;
  const previousDuplicateResult = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.previousData?.findDuplicateSceneGroups
    : unfilteredDuplicateQuery.previousData?.findDuplicateSceneGroups;
  const displayDuplicateResult = duplicateResult ?? previousDuplicateResult;
  const duplicateGroups = duplicateResult?.groups;
  const duplicateCount = displayDuplicateResult?.count ?? 0;
  const duplicateLoading = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.loading
    : unfilteredDuplicateQuery.loading;
  const duplicateError = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.error
    : unfilteredDuplicateQuery.error;
  const hasDuplicateData = duplicateResult !== undefined;
  const tableLoading = duplicateLoading && !hasDuplicateData;

  const missingPhashQuery = useQuery(GQL.FindScenesDocument, {
    variables: {
      filter: { per_page: 0 },
      scene_filter_ast: {
        root: {
          group: {
            operator: GQL.FilterGroupOperator.And,
            children: [
              {
                condition: {
                  field: "is_missing",
                  value: "phash",
                },
              },
              {
                condition: {
                  field: "file_count",
                  value: {
                    modifier: GQL.CriterionModifier.GreaterThan,
                    value: 0,
                  },
                },
              },
            ],
          },
        },
      },
    },
  });

  const [destroyScenes] = useMutation(GQL.ScenesDestroyDocument);

  const allGroups = useMemo(() => {
    const groups = duplicateGroups ?? [];
    return [...groups]
      .map((group) => [...group])
      .sort((a, b) => sceneGroupSize(b) - sceneGroupSize(a));
  }, [duplicateGroups]);

  const totalPages = pageCount(duplicateCount, pageSize);
  const page = Math.min(currentPage, totalPages);
  const pagedGroups = allGroups;

  const selectedScenes = useMemo(
    () => allGroups.flat().filter((scene) => checkedScenes[scene.id]),
    [allGroups, checkedScenes],
  );

  const deleteDialogScenes =
    deleteTargetScenes.length > 0 ? deleteTargetScenes : selectedScenes;

  const selectedPaths = deleteDialogScenes.flatMap((scene) =>
    scene.files.map((file) => file.path),
  );

  const missingPhashes = missingPhashQuery.data?.findScenes.count ?? 0;

  function setSearch(next: {
    page?: number;
    size?: number;
    distance?: number;
    durationDiff?: number;
    filterScope?: DuplicateFilterScope;
  }) {
    void navigate({
      search: (prev) => ({
        ...prev,
        page:
          next.page === undefined || next.page === DEFAULT_PAGE
            ? undefined
            : next.page,
        size:
          next.size === undefined || next.size === DEFAULT_PAGE_SIZE
            ? undefined
            : next.size,
        distance:
          next.distance === undefined || next.distance === DEFAULT_DISTANCE
            ? undefined
            : next.distance,
        durationDiff:
          next.durationDiff === undefined ||
          next.durationDiff === DEFAULT_DURATION_DIFF
            ? undefined
            : next.durationDiff,
        filterScope:
          next.filterScope === undefined
            ? prev.filterScope
            : next.filterScope === DEFAULT_DUPLICATE_FILTER_SCOPE
              ? undefined
              : next.filterScope,
      }),
    });
  }

  function setPage(newPage: number) {
    setSearch({
      page: newPage,
      size: pageSize,
      distance: hashDistance,
      durationDiff,
    });
    setCheckedScenes({});
  }

  function setDistance(distance: number) {
    setSearch({ page: DEFAULT_PAGE, size: pageSize, distance, durationDiff });
    setCheckedScenes({});
  }

  function setDurationDiff(diff: number) {
    setSearch({
      page: DEFAULT_PAGE,
      size: pageSize,
      distance: hashDistance,
      durationDiff: diff,
    });
    setCheckedScenes({});
  }

  function setSize(size: number) {
    setSearch({
      page: DEFAULT_PAGE,
      size,
      distance: hashDistance,
      durationDiff,
    });
    setCheckedScenes({});
  }

  function setFilterScope(scope: DuplicateFilterScope) {
    setSearch({
      page: DEFAULT_PAGE,
      size: pageSize,
      distance: hashDistance,
      durationDiff,
      filterScope: scope,
    });
    setCheckedScenes({});
  }

  useEffect(() => {
    if (!hasDuplicateData || currentPage <= totalPages) return;

    setCheckedScenes({});
    void navigate({
      search: (prev) => ({
        ...prev,
        page: totalPages === DEFAULT_PAGE ? undefined : totalPages,
      }),
      replace: true,
    });
  }, [currentPage, hasDuplicateData, navigate, totalPages]);

  function checkScenes(scenes: SceneDuplicate[]) {
    setCheckedScenes(
      Object.fromEntries(scenes.map((scene) => [scene.id, true])),
    );
  }

  function selectAllButByGroup(
    keep: (group: SceneGroup) => SceneDuplicate | undefined,
  ) {
    const scenes: SceneDuplicate[] = [];
    for (const group of pagedGroups) {
      if (safeSelect && !sameCodec(group)) continue;
      const retained = keep(group);
      if (!retained) continue;
      for (const scene of group) {
        if (scene !== retained) scenes.push(scene);
      }
    }
    checkScenes(scenes);
  }

  function openDeleteDialog() {
    setDeleteTargetScenes(selectedScenes);
    setDeleteOpen(true);
  }

  function setDeleteDialogOpen(open: boolean) {
    setDeleteOpen(open);
    if (!open) setDeleteTargetScenes([]);
  }

  async function handleDelete(options: DeleteOptions) {
    const scenesToDelete =
      deleteTargetScenes.length > 0 ? deleteTargetScenes : selectedScenes;

    try {
      await destroyScenes({
        variables: {
          ids: scenesToDelete.map((scene) => scene.id),
          delete_file: options.deleteFile,
          delete_generated: options.deleteGenerated,
        },
      });
      setCheckedScenes({});
      await refetchDuplicateResults();
      toast.success(
        intl.formatMessage(
          {
            id: "toast.delete_past_tense",
            defaultMessage:
              "Deleted {count, plural, one {{singularEntity}} other {{pluralEntity}}}",
          },
          {
            count: scenesToDelete.length,
            singularEntity: "scene",
            pluralEntity: "scenes",
          },
        ),
      );
    } catch (error) {
      toast.error(error);
      throw error;
    }
  }

  async function refetchDuplicateResults(): Promise<void> {
    const refetchDuplicateQuery = shouldFilterDuplicateQuery
      ? filteredDuplicateQuery.refetch()
      : unfilteredDuplicateQuery.refetch();

    await refetchDuplicateQuery;
  }

  if (duplicateError || (!duplicateLoading && !hasDuplicateData)) {
    return (
      <div className="p-6">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <AlertTriangle />
            </EmptyMedia>
            <EmptyTitle>
              {intl.formatMessage({
                id: "errors.loading_failed",
                defaultMessage: "Loading failed",
              })}
            </EmptyTitle>
            <EmptyDescription>
              {duplicateError?.message ??
                "Error searching for duplicate scenes."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden p-3 lg:p-4">
        <div className="flex min-h-0 flex-1 flex-col lg:flex-row lg:items-stretch">
          <ToolFilterSidebar
            mode={GQL.FilterMode.Scenes}
            open={filterOpen}
            filter={filterModel}
            setFilter={setFilter}
            onOpenChange={setFilterOpen}
            applyMode="manual"
          />

          <Card className="min-h-0 min-w-0 flex-1 gap-0 py-0">
            <CardHeader className="shrink-0 gap-3 border-b py-3 sm:grid-cols-[minmax(0,1fr)_auto]">
              <div>
                <CardTitle>
                  <FormattedMessage
                    id="dupe_check.title"
                    defaultMessage="Duplicate scenes"
                  />
                </CardTitle>
                <CardDescription className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span>
                    <FormattedMessage
                      id="dupe_check.found_sets"
                      defaultMessage="{setCount, plural, one {# set of duplicates found.} other {# sets of duplicates found.}}"
                      values={{ setCount: duplicateCount }}
                    />
                  </span>
                  {missingPhashes > 0 && (
                    <span className="inline-flex items-center gap-1 text-amber-700 dark:text-amber-300">
                      <AlertTriangle className="size-3.5 shrink-0" />
                      <FormattedMessage
                        id="dupe_check.scene_missing_phash_warning"
                        defaultMessage="{count, plural, one {# scene is missing a perceptual hash.} other {# scenes are missing perceptual hashes.}} Please run the phash generation task."
                        values={{ count: missingPhashes }}
                      />
                    </span>
                  )}
                </CardDescription>
              </div>
              <div className="flex flex-col items-end gap-2">
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {selectedScenes.length > 0 && (
                    <>
                      <span className="text-sm text-muted-foreground">
                        {intl.formatMessage(
                          {
                            id: "count_selected",
                            defaultMessage:
                              "{count, plural, one {# selected} other {# selected}}",
                          },
                          { count: selectedScenes.length },
                        )}
                      </span>
                      <Button
                        type="button"
                        variant="secondary"
                        size="sm"
                        onClick={() => setBulkEditOpen(true)}
                      >
                        <Pencil />
                        <FormattedMessage
                          id="actions.edit"
                          defaultMessage="Edit"
                        />
                      </Button>
                      <Button
                        type="button"
                        variant="destructive"
                        size="sm"
                        onClick={openDeleteDialog}
                      >
                        <Trash2 />
                        <FormattedMessage
                          id="actions.delete"
                          defaultMessage="Delete"
                        />
                      </Button>
                    </>
                  )}
                  <Button
                    type="button"
                    variant={filterOpen ? "secondary" : "outline"}
                    size="sm"
                    onClick={() => setFilterOpen((open) => !open)}
                  >
                    <Filter />
                    <FormattedMessage
                      id="search_filter.name"
                      defaultMessage="Filter"
                    />
                    {filterModel.count() > 0 && (
                      <Badge variant="secondary">{filterModel.count()}</Badge>
                    )}
                  </Button>
                  {duplicateCount > 0 && (
                    <>
                      <Select
                        value={String(pageSize)}
                        onValueChange={(value) => setSize(Number(value))}
                      >
                        <SelectTrigger size="sm" className="min-w-24">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {PAGE_SIZES.filter((size, index) => {
                            return (
                              duplicateCount > size ||
                              index === 0 ||
                              duplicateCount > PAGE_SIZES[index - 1]
                            );
                          }).map((size) => (
                            <SelectItem key={size} value={String(size)}>
                              {size}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={page <= 1}
                        onClick={() => setPage(page - 1)}
                      >
                        <FormattedMessage
                          id="actions.previous"
                          defaultMessage="Previous"
                        />
                      </Button>
                      <span className="min-w-20 text-center text-sm text-muted-foreground">
                        {page} / {totalPages}
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={page >= totalPages}
                        onClick={() => setPage(page + 1)}
                      >
                        <FormattedMessage
                          id="actions.next"
                          defaultMessage="Next"
                        />
                      </Button>
                    </>
                  )}
                </div>

                <div className="flex flex-wrap items-center justify-end gap-2">
                  <DuplicateFilterScopeToggle
                    value={filterScope}
                    onValueChange={setFilterScope}
                  />

                  <Label
                    htmlFor={`${controlId}-accuracy`}
                    className="flex items-center gap-2 text-sm"
                  >
                    <span className="text-muted-foreground">
                      <FormattedMessage
                        id="dupe_check.search_accuracy_label"
                        defaultMessage="Search accuracy"
                      />
                    </span>
                    <Select
                      value={String(hashDistance)}
                      onValueChange={(value) => setDistance(Number(value))}
                    >
                      <SelectTrigger
                        id={`${controlId}-accuracy`}
                        size="sm"
                        className="min-w-28"
                      >
                        <SelectValue>
                          {accuracyLabel(hashDistance, intl.formatMessage)}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {SCENE_ACCURACY_OPTIONS.map((option) => (
                          <SelectItem
                            key={option.value}
                            value={String(option.value)}
                          >
                            {intl.formatMessage({
                              id: option.id,
                              defaultMessage: option.defaultMessage,
                            })}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Label>

                  <Label
                    htmlFor={`${controlId}-duration`}
                    className="flex items-center gap-2 text-sm"
                  >
                    <span className="text-muted-foreground">
                      <FormattedMessage
                        id="dupe_check.duration_diff"
                        defaultMessage="Maximum duration difference"
                      />
                    </span>
                    <Select
                      value={String(durationDiff)}
                      onValueChange={(value) => setDurationDiff(Number(value))}
                    >
                      <SelectTrigger
                        id={`${controlId}-duration`}
                        size="sm"
                        className="min-w-24"
                      >
                        <SelectValue>
                          {durationLabel(durationDiff, intl.formatMessage)}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {SCENE_DURATION_OPTIONS.map((option) => (
                          <SelectItem
                            key={option.value}
                            value={String(option.value)}
                          >
                            {durationLabel(option.value, intl.formatMessage)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Label>

                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={<Button variant="outline" size="sm" />}
                    >
                      <CheckSquare />
                      <FormattedMessage
                        id="dupe_check.select_options"
                        defaultMessage="Select"
                      />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="min-w-56">
                      <DropdownMenuItem onClick={() => setCheckedScenes({})}>
                        <XSquare />
                        <FormattedMessage
                          id="dupe_check.select_none"
                          defaultMessage="Select none"
                        />
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() =>
                          selectAllButByGroup((group) =>
                            sameResolution(group)
                              ? undefined
                              : group.reduce((best, scene) =>
                                  sceneResolution(scene) > sceneResolution(best)
                                    ? scene
                                    : best,
                                ),
                          )
                        }
                      >
                        <Ruler />
                        <FormattedMessage
                          id="dupe_check.select_all_but_largest_resolution"
                          defaultMessage="All but largest resolution"
                        />
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() =>
                          selectAllButByGroup((group) =>
                            group.reduce((best, scene) =>
                              sceneFileSize(scene) > sceneFileSize(best)
                                ? scene
                                : best,
                            ),
                          )
                        }
                      >
                        <FileVideo />
                        <FormattedMessage
                          id="dupe_check.select_all_but_largest_file"
                          defaultMessage="All but largest file"
                        />
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() =>
                          selectAllButByGroup((group) =>
                            newestOrOldestScene(group, true),
                          )
                        }
                      >
                        <CalendarArrowDown />
                        <FormattedMessage
                          id="dupe_check.select_oldest"
                          defaultMessage="Oldest"
                        />
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() =>
                          selectAllButByGroup((group) =>
                            newestOrOldestScene(group, false),
                          )
                        }
                      >
                        <CalendarArrowUp />
                        <FormattedMessage
                          id="dupe_check.select_youngest"
                          defaultMessage="Youngest"
                        />
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>

                <Label className="flex items-center gap-2 text-sm font-normal text-muted-foreground">
                  <Checkbox
                    checked={safeSelect}
                    onCheckedChange={(v) => {
                      setSafeSelect(v === true);
                      setCheckedScenes({});
                    }}
                  />
                  <FormattedMessage
                    id="dupe_check.only_select_matching_codecs"
                    defaultMessage="Only select if all codecs match in the duplicate group"
                  />
                </Label>
              </div>
            </CardHeader>

            <CardContent className="min-h-0 flex-1 px-0">
              {tableLoading ? (
                <DuplicateTable
                  groups={[]}
                  loading
                  checkedScenes={checkedScenes}
                  onCheckedChange={(id, checked) =>
                    setCheckedScenes((prev) => ({ ...prev, [id]: checked }))
                  }
                  onMergeGroup={setMergeGroup}
                  onPreviewClick={(group, index) =>
                    setLightbox({
                      slides: group.map((scene) => ({
                        type: "scene" as const,
                        sceneId: scene.id,
                        title: objectTitle(scene) || undefined,
                        posterSrc: scene.paths.screenshot ?? undefined,
                      })),
                      index,
                    })
                  }
                />
              ) : allGroups.length === 0 ? (
                <Empty className="h-full min-h-64">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <Clapperboard />
                    </EmptyMedia>
                    <EmptyTitle>
                      <FormattedMessage
                        id="dupe_check.no_scenes_found"
                        defaultMessage="No duplicate scenes found."
                      />
                    </EmptyTitle>
                  </EmptyHeader>
                </Empty>
              ) : (
                <DuplicateTable
                  groups={pagedGroups}
                  checkedScenes={checkedScenes}
                  onCheckedChange={(id, checked) =>
                    setCheckedScenes((prev) => ({ ...prev, [id]: checked }))
                  }
                  onMergeGroup={setMergeGroup}
                  onPreviewClick={(group, index) =>
                    setLightbox({
                      slides: group.map((scene) => ({
                        type: "scene" as const,
                        sceneId: scene.id,
                        title: objectTitle(scene) || undefined,
                        posterSrc: scene.paths.screenshot ?? undefined,
                      })),
                      index,
                    })
                  }
                />
              )}
            </CardContent>
          </Card>
        </div>
      </div>

      <SceneLightbox
        open={lightbox !== null}
        onClose={() => setLightbox(null)}
        slides={lightbox?.slides ?? []}
        index={lightbox?.index ?? 0}
      />

      <SceneBulkEditSheet
        open={bulkEditOpen}
        onOpenChange={setBulkEditOpen}
        items={selectedScenes}
        onSaved={() => {
          setCheckedScenes({});
          void refetchDuplicateResults();
        }}
      />

      <SceneMergeDialog
        open={mergeGroup !== null}
        onOpenChange={(open) => {
          if (!open) setMergeGroup(null);
        }}
        sources={mergeGroup ?? []}
      />

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteDialogOpen}
        entityCountLabel={intl.formatMessage(
          {
            id: "dialogs.delete_scenes_count",
            defaultMessage: "{count, plural, one {# scene} other {# scenes}}",
          },
          { count: deleteDialogScenes.length },
        )}
        showFileOptions
        details={<DeleteFilesList paths={selectedPaths} />}
        detailsLabel={intl.formatMessage(
          {
            id: "dialogs.delete_show_files_count",
            defaultMessage:
              "Show {count, plural, one {# file} other {# files}}",
          },
          { count: selectedPaths.length },
        )}
        onConfirm={handleDelete}
      />
    </div>
  );
}

function SortableSceneTableHead({
  column,
  sort,
  onSort,
  className,
  children,
}: {
  column: SceneSortColumn;
  sort: SceneSortState | undefined;
  onSort: (column: SceneSortColumn) => void;
  className?: string;
  children: ReactNode;
}) {
  const active = sort?.column === column;
  const sortDirection = active ? sort.direction : undefined;
  const Icon =
    sortDirection === "asc"
      ? ArrowUp
      : sortDirection === "desc"
        ? ArrowDown
        : ArrowUpDown;

  return (
    <TableHead
      className={cn("sticky top-0 z-10 bg-card", className)}
      aria-sort={
        sortDirection === "asc"
          ? "ascending"
          : sortDirection === "desc"
            ? "descending"
            : undefined
      }
    >
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={cn(
          "h-auto gap-1 px-0 py-0 font-medium hover:bg-transparent",
          className?.includes("text-right") && "ml-auto",
        )}
        onClick={() => onSort(column)}
      >
        <span>{children}</span>
        <Icon className="size-3 text-muted-foreground" />
      </Button>
    </TableHead>
  );
}

function DuplicateTable({
  groups,
  loading = false,
  checkedScenes,
  onCheckedChange,
  onMergeGroup,
  onPreviewClick,
}: {
  groups: SceneGroup[];
  loading?: boolean;
  checkedScenes: Record<string, boolean>;
  onCheckedChange: (id: string, checked: boolean) => void;
  onMergeGroup: (group: SceneGroup) => void;
  onPreviewClick: (group: SceneGroup, index: number) => void;
}) {
  const intl = useIntl();
  const [sort, setSort] = useState<SceneSortState>();
  const sortedGroups = useMemo(() => {
    if (!sort) return groups;

    const direction = sort.direction === "asc" ? 1 : -1;
    return groups.map((group) =>
      [...group].sort((a, b) => {
        const result =
          direction * compareScenes(a, b, sort.column, intl.locale);
        return (
          result || compareText(objectTitle(a), objectTitle(b), intl.locale)
        );
      }),
    );
  }, [groups, intl.locale, sort]);
  const tintBySceneId = useMemo(
    () =>
      sort && sort.column !== "details"
        ? sceneSortTintClasses(sortedGroups, sort.column)
        : undefined,
    [sort, sortedGroups],
  );

  return (
    <Table containerClassName="h-full overflow-auto">
      <TableHeader>
        <TableRow>
          <TableHead className="sticky top-0 z-10 w-14 bg-card px-3 text-center" />
          <TableHead className="sticky top-0 z-10 w-28 bg-card" />
          <SortableSceneTableHead
            column="details"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
          >
            <FormattedMessage id="details" defaultMessage="Details" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="metadata"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
          >
            <FormattedMessage id="metadata" defaultMessage="Metadata" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="duration"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="duration" defaultMessage="Duration" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="filesize"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="filesize" defaultMessage="File Size" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="resolution"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="resolution" defaultMessage="Resolution" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="bitrate"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="bitrate" defaultMessage="Bit Rate" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="color"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
          >
            <FormattedMessage id="media_info.color" defaultMessage="Color" />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="videoCodec"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
          >
            <FormattedMessage
              id="media_info.video_codec"
              defaultMessage="Video Codec"
            />
          </SortableSceneTableHead>
          <SortableSceneTableHead
            column="audioCodec"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextSceneSort(current, column))
            }
          >
            <FormattedMessage
              id="media_info.audio_codec"
              defaultMessage="Audio Codec"
            />
          </SortableSceneTableHead>
          <TableHead className="sticky top-0 z-10 w-24 bg-card" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <SceneTableSkeletonRows />
        ) : (
          sortedGroups.map((group, groupIndex) =>
            group.map((scene, sceneIndex) => {
              const file = primaryFile(scene);
              const checked = checkedScenes[scene.id] ?? false;
              const sortTint = tintBySceneId?.get(scene.id);
              const groupDivider = sceneIndex === 0 && groupIndex > 0;
              return (
                <TableRow
                  key={scene.id}
                  className={cn(
                    groupDivider && "border-t-4",
                    groupDivider &&
                      (sortTint ? "border-t-background" : "border-t-border"),
                    sortTint,
                    checked && "bg-destructive/5",
                  )}
                >
                  <TableCell className="w-14 !px-3">
                    <div className="flex justify-center">
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(v) =>
                          onCheckedChange(scene.id, v === true)
                        }
                        aria-label={intl.formatMessage(
                          {
                            id: "actions.select_entity",
                            defaultMessage: "Select {entityType}",
                          },
                          { entityType: objectTitle(scene) },
                        )}
                      />
                    </div>
                  </TableCell>
                  <TableCell>
                    <Button
                      type="button"
                      variant="ghost"
                      className="h-auto p-0"
                      onClick={() => onPreviewClick(group, sceneIndex)}
                      aria-label={intl.formatMessage(
                        {
                          id: "actions.preview_entity",
                          defaultMessage: "Preview {entityType}",
                        },
                        { entityType: objectTitle(scene) },
                      )}
                    >
                      <img
                        src={scene.paths.screenshot ?? ""}
                        alt=""
                        className={cn(
                          "h-16 w-28 rounded-md border object-cover",
                          checked &&
                            "border-destructive ring-2 ring-destructive",
                        )}
                      />
                    </Button>
                  </TableCell>
                  <TableCell className="min-w-64 max-w-md whitespace-normal">
                    <Link
                      to="/scenes/$sceneId"
                      params={{ sceneId: scene.id }}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={cn(
                        "font-medium hover:underline",
                        checked && "text-destructive line-through decoration-2",
                      )}
                    >
                      {objectTitle(scene)}
                    </Link>
                    <div
                      className="mt-1 break-all font-mono text-xs text-muted-foreground"
                      data-selectable-text
                    >
                      {file?.path ?? ""}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-normal">
                    <div className="flex flex-wrap gap-1">
                      {scene.tags.length > 0 && (
                        <MetadataBadge
                          items={scene.tags.map((tag) => tag.name)}
                        >
                          <FormattedMessage
                            id="dupe_check.tags_count"
                            defaultMessage="{count, plural, one {# tag} other {# tags}}"
                            values={{ count: scene.tags.length }}
                          />
                        </MetadataBadge>
                      )}
                      {scene.performers.length > 0 && (
                        <MetadataBadge
                          items={scene.performers.map((performer) =>
                            performer.disambiguation
                              ? `${performer.name} (${performer.disambiguation})`
                              : performer.name,
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.performers_count"
                            defaultMessage="{count, plural, one {# performer} other {# performers}}"
                            values={{ count: scene.performers.length }}
                          />
                        </MetadataBadge>
                      )}
                      {scene.groups.length > 0 && (
                        <MetadataBadge
                          items={scene.groups.map(
                            (sceneGroup) => sceneGroup.group.name,
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.groups_count"
                            defaultMessage="{count, plural, one {# group} other {# groups}}"
                            values={{ count: scene.groups.length }}
                          />
                        </MetadataBadge>
                      )}
                      {scene.scene_markers.length > 0 && (
                        <MetadataBadge
                          items={scene.scene_markers.map(
                            (marker) =>
                              `${secondsToTimestamp(marker.seconds)} ${marker.title} (${marker.primary_tag.name})`,
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.markers_count"
                            defaultMessage="{count, plural, one {# marker} other {# markers}}"
                            values={{ count: scene.scene_markers.length }}
                          />
                        </MetadataBadge>
                      )}
                      {scene.galleries.length > 0 && (
                        <MetadataBadge
                          items={scene.galleries.map((gallery) =>
                            objectTitle(gallery),
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.galleries_count"
                            defaultMessage="{count, plural, one {# gallery} other {# galleries}}"
                            values={{ count: scene.galleries.length }}
                          />
                        </MetadataBadge>
                      )}
                      {(scene.o_counter ?? 0) > 0 && (
                        <Badge variant="secondary">
                          {intl.formatMessage({
                            id: "o_count",
                            defaultMessage: "O Count",
                          })}
                          : {scene.o_counter}
                        </Badge>
                      )}
                      {scene.files.length > 1 && (
                        <Badge variant="outline">
                          <FormattedMessage
                            id="files_amount"
                            defaultMessage="{value} files"
                            values={{ value: scene.files.length }}
                          />
                        </Badge>
                      )}
                      {scene.organized && (
                        <Badge variant="outline">
                          <FormattedMessage
                            id="organized"
                            defaultMessage="Organized"
                          />
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {file?.duration ? secondsToTimestamp(file.duration) : ""}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatBytes(file?.size)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {file ? `${file.width ?? 0}x${file.height ?? 0}` : "N/A"}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    <FormattedNumber
                      value={(file?.bit_rate ?? 0) / 1000000}
                      maximumFractionDigits={2}
                    />
                    &nbsp;mbps
                  </TableCell>
                  <TableCell>
                    <MediaColorBadge file={file} />
                  </TableCell>
                  <TableCell>{file?.video_codec ?? ""}</TableCell>
                  <TableCell>
                    {file?.audio_codec ? (
                      file.audio_codec
                    ) : (
                      <FormattedMessage id="none" defaultMessage="None" />
                    )}
                  </TableCell>
                  {sceneIndex === 0 && (
                    <TableCell rowSpan={group.length} className="align-middle">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => onMergeGroup(group)}
                      >
                        <GitMerge />
                        <FormattedMessage
                          id="actions.merge"
                          defaultMessage="Merge"
                        />
                      </Button>
                    </TableCell>
                  )}
                </TableRow>
              );
            }),
          )
        )}
      </TableBody>
    </Table>
  );
}

function SceneTableSkeletonRows() {
  return Array.from({ length: TABLE_SKELETON_ROWS }, (_, index) => (
    <TableRow key={`scene-skeleton-${index}`}>
      <TableCell className="w-14 !px-3">
        <div className="flex justify-center">
          <Skeleton className="size-4" />
        </div>
      </TableCell>
      <TableCell>
        <Skeleton className="h-16 w-28 rounded-md" />
      </TableCell>
      <TableCell className="min-w-64 max-w-md">
        <div className="space-y-2">
          <Skeleton className="h-4 w-48 max-w-full" />
          <Skeleton className="h-3 w-72 max-w-full" />
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          <Skeleton className="h-5 w-16" />
          <Skeleton className="h-5 w-20" />
          <Skeleton className="h-5 w-14" />
        </div>
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-14" />
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-16" />
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-20" />
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-16" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-5 w-16" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-4 w-20" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-4 w-20" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-7 w-20" />
      </TableCell>
    </TableRow>
  ));
}

export const Route = createFileRoute("/scene-duplicate-checker")({
  validateSearch: searchSchema,
  component: SceneDuplicateCheckerPage,
});
