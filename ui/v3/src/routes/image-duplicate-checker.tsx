import { type ReactNode, useEffect, useMemo, useState } from "react";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useMutation, useQuery } from "@apollo/client/react";
import { FormattedMessage, useIntl } from "react-intl";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  CalendarArrowDown,
  CalendarArrowUp,
  CheckSquare,
  Filter,
  ImageIcon,
  Pencil,
  Ruler,
  Trash2,
  XSquare,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { imageTitle, objectTitle } from "src/core/files";
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
import { Lightbox, type LightboxSlide } from "src/components/lightbox";
import {
  fileSize,
  fileSizeFractionalDigits,
  formatFileSizeUnit,
} from "src/utils/file";
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "src/components/ui/tooltip";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { Skeleton } from "src/components/ui/skeleton";
import {
  DeleteDialog,
  DeleteFilesList,
  type DeleteOptions,
} from "src/components/detail/delete-dialog";
import { ImageBulkEditSheet } from "src/components/detail/image-bulk-edit-sheet";
import { useToast } from "src/hooks/toast";

const DEFAULT_PAGE = 1;
const DEFAULT_PAGE_SIZE = 20;
const DEFAULT_DISTANCE = 0;

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

const IMAGE_ACCURACY_OPTIONS = [
  { value: 0, id: "dupe_check.options.exact", defaultMessage: "Exact" },
  { value: 4, id: "dupe_check.options.high", defaultMessage: "High" },
  { value: 8, id: "dupe_check.options.medium", defaultMessage: "Medium" },
  { value: 10, id: "dupe_check.options.low", defaultMessage: "Low" },
] as const;

const searchSchema = z.object({
  page: z.coerce.number().int().positive().optional(),
  size: z.coerce.number().int().positive().optional(),
  distance: z.coerce.number().int().min(0).optional(),
  fa: z.string().optional(),
  filterScope: z.enum(DUPLICATE_FILTER_SCOPES).optional().catch(undefined),
});

type ImageDuplicate =
  GQL.FindDuplicateImagesQuery["findDuplicateImageGroups"]["groups"][number][number];
type ImageGroup = ImageDuplicate[];
type ImageDuplicateFile = ImageDuplicate["visual_files"][number];
type ImageSortColumn =
  | "details"
  | "metadata"
  | "filesize"
  | "resolution"
  | "color";
type SortDirection = "asc" | "desc";
type ImageSortState = {
  column: ImageSortColumn;
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

function nextImageSort(
  current: ImageSortState | undefined,
  column: ImageSortColumn,
): ImageSortState {
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

function imageMetadataScore(image: ImageDuplicate): number {
  return (
    image.tags.length +
    image.performers.length +
    image.galleries.length +
    (image.visual_files.length > 1 ? image.visual_files.length : 0) +
    (image.organized ? 1 : 0)
  );
}

function compareImages(
  a: ImageDuplicate,
  b: ImageDuplicate,
  column: ImageSortColumn,
  locale: string,
): number {
  switch (column) {
    case "details": {
      const aFile = primaryFile(a);
      const bFile = primaryFile(b);
      return compareText(
        `${imageTitle(a)} ${aFile?.path ?? ""}`,
        `${imageTitle(b)} ${bFile?.path ?? ""}`,
        locale,
      );
    }
    case "metadata":
      return imageMetadataScore(a) - imageMetadataScore(b);
    case "filesize":
      return imageFileSize(a) - imageFileSize(b);
    case "resolution":
      return imageResolution(a) - imageResolution(b);
    case "color":
      return compareMediaColor(primaryFile(a), primaryFile(b), locale);
  }
}

function stableHash(value: string): number {
  let hash = 0;
  for (let i = 0; i < value.length; i++) {
    hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
  }
  return hash;
}

function imageSortValueKey(
  image: ImageDuplicate,
  column: ImageSortColumn,
): string {
  const file = primaryFile(image);

  switch (column) {
    case "details":
      return `${imageTitle(image).trim().toLocaleLowerCase()}\u0000${(file?.path ?? "").trim().toLocaleLowerCase()}`;
    case "metadata":
      return String(imageMetadataScore(image));
    case "filesize":
      return String(imageFileSize(image));
    case "resolution":
      return String(imageResolution(image));
    case "color":
      return mediaColorValueKey(file);
  }
}

function imageSortTintClasses(
  groups: ImageGroup[],
  column: ImageSortColumn,
): Map<string, string> {
  const tintByImageId = new Map<string, string>();

  for (const group of groups) {
    const counts = new Map<string, number>();
    for (const image of group) {
      const key = imageSortValueKey(image, column);
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }

    if (![...counts.values()].some((count) => count > 1)) continue;

    // Hash only varies the palette offset between groups; within a group each
    // distinct value takes the next palette entry, so two different values
    // can't share a tint until a group exceeds the palette size.
    const groupOffset = stableHash(
      `${group.map((image) => image.id).join(":")}:${column}`,
    );
    const tintByValue = new Map<string, string>();

    for (const image of group) {
      const key = imageSortValueKey(image, column);
      let tint = tintByValue.get(key);
      if (!tint) {
        tint =
          SORT_VALUE_TINT_CLASSES[
            (groupOffset + tintByValue.size) % SORT_VALUE_TINT_CLASSES.length
          ];
        tintByValue.set(key, tint);
      }
      tintByImageId.set(image.id, tint);
    }
  }

  return tintByImageId;
}

function formatBytes(bytes: number | undefined): string {
  const size = fileSize(bytes ?? 0);
  return `${size.size.toFixed(fileSizeFractionalDigits(size.unit))} ${formatFileSizeUnit(size.unit)}`;
}

function primaryFile(image: ImageDuplicate): ImageDuplicateFile | undefined {
  return image.visual_files[0];
}

function imageFileSize(image: ImageDuplicate): number {
  return image.visual_files.reduce(
    (max, file) => Math.max(max, file.size ?? 0),
    0,
  );
}

function imageResolution(image: ImageDuplicate): number {
  return image.visual_files.reduce(
    (max, file) => Math.max(max, (file.width ?? 0) * (file.height ?? 0)),
    0,
  );
}

function imageGroupSize(group: ImageGroup): number {
  return group.reduce(
    (total, image) =>
      total +
      image.visual_files.reduce(
        (fileTotal, file) => fileTotal + (file.size ?? 0),
        0,
      ),
    0,
  );
}

function newestOrOldestImage(
  group: ImageGroup,
  oldest: boolean,
): ImageDuplicate | undefined {
  let selected: ImageDuplicate | undefined;
  let selectedTime: number | undefined;

  for (const image of group) {
    for (const file of image.visual_files) {
      const time = new Date(file.mod_time).getTime();
      if (!Number.isFinite(time)) continue;
      if (
        selectedTime === undefined ||
        (oldest ? time < selectedTime : time > selectedTime)
      ) {
        selected = image;
        selectedTime = time;
      }
    }
  }

  return selected;
}

function sameResolution(group: ImageGroup): boolean {
  return new Set(group.map(imageResolution)).size === 1;
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
  const option = IMAGE_ACCURACY_OPTIONS.find((item) => item.value === value);
  if (!option) return String(value);

  return formatMessage({
    id: option.id,
    defaultMessage: option.defaultMessage,
  });
}

function ImageDuplicateCheckerPage() {
  const intl = useIntl();
  const navigate = useNavigate({ from: Route.fullPath });
  const toast = useToast();
  const search = Route.useSearch();

  const currentPage = search.page ?? DEFAULT_PAGE;
  const pageSize = search.size ?? DEFAULT_PAGE_SIZE;
  const hashDistance = search.distance ?? DEFAULT_DISTANCE;
  const filterScope = search.filterScope ?? DEFAULT_DUPLICATE_FILTER_SCOPE;

  const [checkedImages, setCheckedImages] = useState<Record<string, boolean>>(
    {},
  );
  const [bulkEditOpen, setBulkEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTargetImages, setDeleteTargetImages] = useState<
    ImageDuplicate[]
  >([]);
  const [filterOpen, setFilterOpen] = useState(() => Boolean(search.fa));
  const [lightbox, setLightbox] = useState<{
    slides: LightboxSlide[];
    index: number;
  } | null>(null);

  const [filterModel, setFilterModel] = useState(() => {
    const model = new ListFilterModel(GQL.FilterMode.Images);
    if (search.fa) model.configureFromDecodedParams({ fa: search.fa });
    return model;
  });

  function setFilter(next: ListFilterModel) {
    setFilterModel(next);
    setCheckedImages({});
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
      image_filter: filterModel.makeFilter() as GQL.ImageFilterType,
      image_filter_ast: filterModel.makeFilterAST(),
    }),
    [filterModel],
  );
  const hasFilter = filterModel.count() > 0;
  const shouldFilterDuplicateQuery = hasFilter;
  const duplicateVariables = useMemo(
    () => ({
      distance: hashDistance,
      filter: {
        page: currentPage,
        per_page: pageSize,
      },
    }),
    [currentPage, hashDistance, pageSize],
  );
  const filteredDuplicateVariables = useMemo(
    () => ({
      distance: hashDistance,
      filter: {
        page: currentPage,
        per_page: pageSize,
      },
      filter_mode: duplicateFilterMode(filterScope),
      ...filterVariables,
    }),
    [currentPage, filterScope, filterVariables, hashDistance, pageSize],
  );

  // cache-first: revisiting the page within a session reuses the previous
  // result instead of recomputing all duplicates; the Refresh button
  // refetches explicitly.
  const unfilteredDuplicateQuery = useQuery(GQL.FindDuplicateImagesDocument, {
    variables: duplicateVariables,
    fetchPolicy: "cache-first",
    notifyOnNetworkStatusChange: true,
    skip: shouldFilterDuplicateQuery,
  });
  const filteredDuplicateQuery = useQuery(
    GQL.FindDuplicateImagesFilteredDocument,
    {
      variables: filteredDuplicateVariables,
      fetchPolicy: "cache-first",
      notifyOnNetworkStatusChange: true,
      skip: !shouldFilterDuplicateQuery,
    },
  );
  const duplicateResult = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.data?.findDuplicateImageGroups
    : unfilteredDuplicateQuery.data?.findDuplicateImageGroups;
  const previousDuplicateResult = shouldFilterDuplicateQuery
    ? filteredDuplicateQuery.previousData?.findDuplicateImageGroups
    : unfilteredDuplicateQuery.previousData?.findDuplicateImageGroups;
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

  const missingPhashQuery = useQuery(GQL.FindImagesDocument, {
    variables: {
      filter: { per_page: 0 },
      image_filter: { is_missing: "phash" },
    },
  });

  const [destroyImage] = useMutation(GQL.ImageDestroyDocument);

  const allGroups = useMemo(() => {
    const groups = duplicateGroups ?? [];
    return [...groups]
      .map((group) => [...group])
      .sort((a, b) => imageGroupSize(b) - imageGroupSize(a));
  }, [duplicateGroups]);

  const totalPages = pageCount(duplicateCount, pageSize);
  const page = Math.min(currentPage, totalPages);
  const pagedGroups = allGroups;

  const selectedImages = useMemo(
    () => allGroups.flat().filter((image) => checkedImages[image.id]),
    [allGroups, checkedImages],
  );

  const deleteDialogImages =
    deleteTargetImages.length > 0 ? deleteTargetImages : selectedImages;

  const selectedPaths = deleteDialogImages.flatMap((image) =>
    image.visual_files.map((file) => file.path),
  );

  const missingPhashes = missingPhashQuery.data?.findImages.count ?? 0;

  function setSearch(next: {
    page?: number;
    size?: number;
    distance?: number;
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
    setSearch({ page: newPage, size: pageSize, distance: hashDistance });
    setCheckedImages({});
  }

  function setDistance(distance: number) {
    setSearch({ page: DEFAULT_PAGE, size: pageSize, distance });
    setCheckedImages({});
  }

  function setSize(size: number) {
    setSearch({ page: DEFAULT_PAGE, size, distance: hashDistance });
    setCheckedImages({});
  }

  function setFilterScope(scope: DuplicateFilterScope) {
    setSearch({
      page: DEFAULT_PAGE,
      size: pageSize,
      distance: hashDistance,
      filterScope: scope,
    });
    setCheckedImages({});
  }

  useEffect(() => {
    if (!hasDuplicateData || currentPage <= totalPages) return;

    setCheckedImages({});
    void navigate({
      search: (prev) => ({
        ...prev,
        page: totalPages === DEFAULT_PAGE ? undefined : totalPages,
      }),
      replace: true,
    });
  }, [currentPage, hasDuplicateData, navigate, totalPages]);

  function checkImages(images: ImageDuplicate[]) {
    setCheckedImages(
      Object.fromEntries(images.map((image) => [image.id, true])),
    );
  }

  function selectAllButByGroup(
    keep: (group: ImageGroup) => ImageDuplicate | undefined,
  ) {
    const images: ImageDuplicate[] = [];
    for (const group of pagedGroups) {
      const retained = keep(group);
      if (!retained) continue;
      for (const image of group) {
        if (image !== retained) images.push(image);
      }
    }
    checkImages(images);
  }

  function openDeleteDialog() {
    setDeleteTargetImages(selectedImages);
    setDeleteOpen(true);
  }

  function setDeleteDialogOpen(open: boolean) {
    setDeleteOpen(open);
    if (!open) setDeleteTargetImages([]);
  }

  async function handleDelete(options: DeleteOptions) {
    const imagesToDelete =
      deleteTargetImages.length > 0 ? deleteTargetImages : selectedImages;

    try {
      await Promise.all(
        imagesToDelete.map((image) =>
          destroyImage({
            variables: {
              id: image.id,
              delete_file: options.deleteFile,
              delete_generated: options.deleteGenerated,
            },
          }),
        ),
      );
      setCheckedImages({});
      await refetchDuplicateResults();
      toast.success(
        intl.formatMessage(
          {
            id: "toast.delete_past_tense",
            defaultMessage:
              "Deleted {count, plural, one {{singularEntity}} other {{pluralEntity}}}",
          },
          {
            count: imagesToDelete.length,
            singularEntity: "image",
            pluralEntity: "images",
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
                "Error searching for duplicate images."}
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
            mode={GQL.FilterMode.Images}
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
                    id="dupe_check.image_title"
                    defaultMessage="Duplicate images"
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
                        id="dupe_check.missing_phash_warning"
                        defaultMessage="{count, plural, one {# image is missing a perceptual hash.} other {# images are missing perceptual hashes.}}"
                        values={{ count: missingPhashes }}
                      />
                    </span>
                  )}
                </CardDescription>
              </div>
              <div className="flex flex-col items-end gap-2">
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {selectedImages.length > 0 && (
                    <>
                      <span className="text-sm text-muted-foreground">
                        {intl.formatMessage(
                          {
                            id: "count_selected",
                            defaultMessage:
                              "{count, plural, one {# selected} other {# selected}}",
                          },
                          { count: selectedImages.length },
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

                  <label className="flex items-center gap-2 text-sm">
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
                      <SelectTrigger size="sm" className="min-w-28">
                        <SelectValue>
                          {accuracyLabel(hashDistance, intl.formatMessage)}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {IMAGE_ACCURACY_OPTIONS.map((option) => (
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
                  </label>

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
                      <DropdownMenuItem onClick={() => setCheckedImages({})}>
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
                              : group.reduce((best, image) =>
                                  imageResolution(image) > imageResolution(best)
                                    ? image
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
                            group.reduce((best, image) =>
                              imageFileSize(image) > imageFileSize(best)
                                ? image
                                : best,
                            ),
                          )
                        }
                      >
                        <ImageIcon />
                        <FormattedMessage
                          id="dupe_check.select_all_but_largest_file"
                          defaultMessage="All but largest file"
                        />
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() =>
                          selectAllButByGroup((group) =>
                            newestOrOldestImage(group, true),
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
                            newestOrOldestImage(group, false),
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
              </div>
            </CardHeader>

            <CardContent className="min-h-0 flex-1 px-0">
              {tableLoading ? (
                <DuplicateTable
                  groups={[]}
                  loading
                  checkedImages={checkedImages}
                  onCheckedChange={(id, checked) =>
                    setCheckedImages((prev) => ({ ...prev, [id]: checked }))
                  }
                  onPreviewClick={(group, index) =>
                    setLightbox({
                      slides: group.map((image) => ({
                        src: image.paths.image ?? "",
                        alt: imageTitle(image),
                        imageId: image.id,
                        imageTitle: imageTitle(image),
                      })),
                      index,
                    })
                  }
                />
              ) : allGroups.length === 0 ? (
                <Empty className="h-full min-h-64">
                  <EmptyHeader>
                    <EmptyMedia variant="icon">
                      <ImageIcon />
                    </EmptyMedia>
                    <EmptyTitle>
                      <FormattedMessage
                        id="dupe_check.no_images_found"
                        defaultMessage="No duplicate images found."
                      />
                    </EmptyTitle>
                  </EmptyHeader>
                </Empty>
              ) : (
                <DuplicateTable
                  groups={pagedGroups}
                  checkedImages={checkedImages}
                  onCheckedChange={(id, checked) =>
                    setCheckedImages((prev) => ({ ...prev, [id]: checked }))
                  }
                  onPreviewClick={(group, index) =>
                    setLightbox({
                      slides: group.map((image) => ({
                        src: image.paths.image ?? "",
                        alt: imageTitle(image),
                        imageId: image.id,
                        imageTitle: imageTitle(image),
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

      <Lightbox
        open={lightbox !== null}
        onClose={() => setLightbox(null)}
        slides={lightbox?.slides ?? []}
        index={lightbox?.index ?? 0}
      />

      <ImageBulkEditSheet
        open={bulkEditOpen}
        onOpenChange={setBulkEditOpen}
        items={selectedImages}
        onSaved={() => {
          setCheckedImages({});
          void refetchDuplicateResults();
        }}
      />

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteDialogOpen}
        entityCountLabel={intl.formatMessage(
          {
            id: "dialogs.delete_images_count",
            defaultMessage: "{count, plural, one {# image} other {# images}}",
          },
          { count: deleteDialogImages.length },
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

function SortableImageTableHead({
  column,
  sort,
  onSort,
  className,
  children,
}: {
  column: ImageSortColumn;
  sort: ImageSortState | undefined;
  onSort: (column: ImageSortColumn) => void;
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
  checkedImages,
  onCheckedChange,
  onPreviewClick,
}: {
  groups: ImageGroup[];
  loading?: boolean;
  checkedImages: Record<string, boolean>;
  onCheckedChange: (id: string, checked: boolean) => void;
  onPreviewClick: (group: ImageGroup, index: number) => void;
}) {
  const intl = useIntl();
  const [sort, setSort] = useState<ImageSortState>();
  const sortedGroups = useMemo(() => {
    if (!sort) return groups;

    const direction = sort.direction === "asc" ? 1 : -1;
    return groups.map((group) =>
      [...group].sort((a, b) => {
        const result =
          direction * compareImages(a, b, sort.column, intl.locale);
        return result || compareText(imageTitle(a), imageTitle(b), intl.locale);
      }),
    );
  }, [groups, intl.locale, sort]);
  const tintByImageId = useMemo(
    () =>
      sort && sort.column !== "details"
        ? imageSortTintClasses(sortedGroups, sort.column)
        : undefined,
    [sort, sortedGroups],
  );

  return (
    <Table containerClassName="h-full overflow-auto">
      <TableHeader>
        <TableRow>
          <TableHead className="sticky top-0 z-10 w-14 bg-card px-3 text-center" />
          <TableHead className="sticky top-0 z-10 w-28 bg-card" />
          <SortableImageTableHead
            column="details"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextImageSort(current, column))
            }
          >
            <FormattedMessage id="details" defaultMessage="Details" />
          </SortableImageTableHead>
          <SortableImageTableHead
            column="metadata"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextImageSort(current, column))
            }
          >
            <FormattedMessage id="metadata" defaultMessage="Metadata" />
          </SortableImageTableHead>
          <SortableImageTableHead
            column="filesize"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextImageSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="filesize" defaultMessage="File Size" />
          </SortableImageTableHead>
          <SortableImageTableHead
            column="resolution"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextImageSort(current, column))
            }
            className="text-right"
          >
            <FormattedMessage id="resolution" defaultMessage="Resolution" />
          </SortableImageTableHead>
          <SortableImageTableHead
            column="color"
            sort={sort}
            onSort={(column) =>
              setSort((current) => nextImageSort(current, column))
            }
          >
            <FormattedMessage id="media_info.color" defaultMessage="Color" />
          </SortableImageTableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <ImageTableSkeletonRows />
        ) : (
          sortedGroups.map((group, groupIndex) =>
            group.map((image, imageIndex) => {
              const file = primaryFile(image);
              const checked = checkedImages[image.id] ?? false;
              const sortTint = tintByImageId?.get(image.id);
              const groupDivider = imageIndex === 0 && groupIndex > 0;
              return (
                <TableRow
                  key={image.id}
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
                          onCheckedChange(image.id, v === true)
                        }
                        aria-label={intl.formatMessage(
                          {
                            id: "actions.select_entity",
                            defaultMessage: "Select {entityType}",
                          },
                          { entityType: imageTitle(image) },
                        )}
                      />
                    </div>
                  </TableCell>
                  <TableCell>
                    <Button
                      type="button"
                      variant="ghost"
                      className="h-auto p-0"
                      onClick={() => onPreviewClick(group, imageIndex)}
                      aria-label={intl.formatMessage(
                        {
                          id: "actions.preview_entity",
                          defaultMessage: "Preview {entityType}",
                        },
                        { entityType: imageTitle(image) },
                      )}
                    >
                      <img
                        src={image.paths.thumbnail ?? ""}
                        alt=""
                        className={cn(
                          "h-24 w-24 rounded-md border object-contain",
                          checked &&
                            "border-destructive ring-2 ring-destructive",
                        )}
                      />
                    </Button>
                  </TableCell>
                  <TableCell className="min-w-64 max-w-md whitespace-normal">
                    <Link
                      to="/images/$imageId"
                      params={{ imageId: image.id }}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={cn(
                        "font-medium hover:underline",
                        checked && "text-destructive line-through decoration-2",
                      )}
                    >
                      {imageTitle(image)}
                    </Link>
                    <div className="mt-1 break-all font-mono text-xs text-muted-foreground">
                      {file?.path ?? ""}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-normal">
                    <div className="flex flex-wrap gap-1">
                      {image.tags.length > 0 && (
                        <MetadataBadge
                          items={image.tags.map((tag) => tag.name)}
                        >
                          <FormattedMessage
                            id="dupe_check.tags_count"
                            defaultMessage="{count, plural, one {# tag} other {# tags}}"
                            values={{ count: image.tags.length }}
                          />
                        </MetadataBadge>
                      )}
                      {image.performers.length > 0 && (
                        <MetadataBadge
                          items={image.performers.map(
                            (performer) => performer.name,
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.performers_count"
                            defaultMessage="{count, plural, one {# performer} other {# performers}}"
                            values={{ count: image.performers.length }}
                          />
                        </MetadataBadge>
                      )}
                      {image.galleries.length > 0 && (
                        <MetadataBadge
                          items={image.galleries.map((gallery) =>
                            objectTitle(gallery),
                          )}
                        >
                          <FormattedMessage
                            id="dupe_check.galleries_count"
                            defaultMessage="{count, plural, one {# gallery} other {# galleries}}"
                            values={{ count: image.galleries.length }}
                          />
                        </MetadataBadge>
                      )}
                      {image.visual_files.length > 1 && (
                        <Badge variant="outline">
                          <FormattedMessage
                            id="files_amount"
                            defaultMessage="{value} files"
                            values={{ value: image.visual_files.length }}
                          />
                        </Badge>
                      )}
                      {image.organized && (
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
                    {formatBytes(file?.size)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {file ? `${file.width ?? 0}x${file.height ?? 0}` : "N/A"}
                  </TableCell>
                  <TableCell>
                    <MediaColorBadge file={file} />
                  </TableCell>
                </TableRow>
              );
            }),
          )
        )}
      </TableBody>
    </Table>
  );
}

function ImageTableSkeletonRows() {
  return Array.from({ length: TABLE_SKELETON_ROWS }, (_, index) => (
    <TableRow key={`image-skeleton-${index}`}>
      <TableCell className="w-14 !px-3">
        <div className="flex justify-center">
          <Skeleton className="size-4" />
        </div>
      </TableCell>
      <TableCell>
        <Skeleton className="h-24 w-24 rounded-md" />
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
        </div>
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-16" />
      </TableCell>
      <TableCell>
        <Skeleton className="ml-auto h-4 w-20" />
      </TableCell>
      <TableCell>
        <Skeleton className="h-5 w-16" />
      </TableCell>
    </TableRow>
  ));
}

export const Route = createFileRoute("/image-duplicate-checker")({
  validateSearch: zodValidator(searchSchema),
  component: ImageDuplicateCheckerPage,
});
