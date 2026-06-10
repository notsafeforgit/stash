import { useMemo, useState } from "react";
import { Link, createFileRoute, useNavigate } from "@tanstack/react-router";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useMutation, useQuery } from "@apollo/client/react";
import { FormattedMessage, useIntl } from "react-intl";
import {
  AlertTriangle,
  CalendarArrowDown,
  CalendarArrowUp,
  CheckSquare,
  ImageIcon,
  Pencil,
  Ruler,
  Trash2,
  XSquare,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
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
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "src/components/ui/empty";
import { Spinner } from "src/components/ui/spinner";
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

const searchSchema = z.object({
  page: z.coerce.number().int().positive().optional(),
  size: z.coerce.number().int().positive().optional(),
  distance: z.coerce.number().int().min(0).optional(),
});

type ImageDuplicate =
  GQL.FindDuplicateImagesQuery["findDuplicateImages"][number][number];
type ImageGroup = ImageDuplicate[];

function formatBytes(bytes: number | undefined): string {
  const size = fileSize(bytes ?? 0);
  return `${size.size.toFixed(fileSizeFractionalDigits(size.unit))} ${formatFileSizeUnit(size.unit)}`;
}

function primaryFile(image: ImageDuplicate) {
  return image.visual_files[0];
}

function imageFileSize(image: ImageDuplicate) {
  return image.visual_files.reduce(
    (max, file) => Math.max(max, file.size ?? 0),
    0,
  );
}

function imageResolution(image: ImageDuplicate) {
  return image.visual_files.reduce(
    (max, file) => Math.max(max, (file.width ?? 0) * (file.height ?? 0)),
    0,
  );
}

function imageGroupSize(group: ImageGroup) {
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

function newestOrOldestImage(group: ImageGroup, oldest: boolean) {
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

function sameResolution(group: ImageGroup) {
  return new Set(group.map(imageResolution)).size === 1;
}

function pageCount(total: number, pageSize: number) {
  return Math.max(1, Math.ceil(total / pageSize));
}

function ImageDuplicateCheckerPage() {
  const intl = useIntl();
  const navigate = useNavigate({ from: Route.fullPath });
  const toast = useToast();
  const search = Route.useSearch();

  const currentPage = search.page ?? DEFAULT_PAGE;
  const pageSize = search.size ?? DEFAULT_PAGE_SIZE;
  const hashDistance = search.distance ?? DEFAULT_DISTANCE;

  const [checkedImages, setCheckedImages] = useState<Record<string, boolean>>(
    {},
  );
  const [bulkEditOpen, setBulkEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const duplicateQuery = useQuery(GQL.FindDuplicateImagesDocument, {
    variables: { distance: hashDistance },
    fetchPolicy: "no-cache",
  });

  const missingPhashQuery = useQuery(GQL.FindImagesDocument, {
    variables: {
      filter: { per_page: 0 },
      image_filter: { is_missing: "phash" },
    },
  });

  const [destroyImage] = useMutation(GQL.ImageDestroyDocument);

  const allGroups = useMemo(() => {
    const groups = duplicateQuery.data?.findDuplicateImages ?? [];
    return [...groups]
      .map((group) => [...group])
      .sort((a, b) => imageGroupSize(b) - imageGroupSize(a));
  }, [duplicateQuery.data?.findDuplicateImages]);

  const totalPages = pageCount(allGroups.length, pageSize);
  const page = Math.min(currentPage, totalPages);
  const pagedGroups = useMemo(() => {
    const start = (page - 1) * pageSize;
    return allGroups.slice(start, start + pageSize);
  }, [allGroups, page, pageSize]);

  const selectedImages = useMemo(
    () => allGroups.flat().filter((image) => checkedImages[image.id]),
    [allGroups, checkedImages],
  );

  const selectedPaths = selectedImages.flatMap((image) =>
    image.visual_files.map((file) => file.path),
  );

  const missingPhashes = missingPhashQuery.data?.findImages.count ?? 0;

  function setSearch(next: {
    page?: number;
    size?: number;
    distance?: number;
  }) {
    void navigate({
      search: {
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
      },
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

  async function handleDelete(options: DeleteOptions) {
    try {
      await Promise.all(
        selectedImages.map((image) =>
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
      await duplicateQuery.refetch();
      toast.success(
        intl.formatMessage(
          {
            id: "toast.delete_past_tense",
            defaultMessage:
              "Deleted {count, plural, one {{singularEntity}} other {{pluralEntity}}}",
          },
          {
            count: selectedImages.length,
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

  if (duplicateQuery.loading) {
    return (
      <div className="grid min-h-[50vh] place-items-center">
        <Spinner className="size-8 text-muted-foreground" />
      </div>
    );
  }

  if (duplicateQuery.error || !duplicateQuery.data) {
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
              {duplicateQuery.error?.message ??
                "Error searching for duplicate images."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    );
  }

  return (
    <div className="space-y-4 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">
            <FormattedMessage
              id="config.tools.image_duplicate_checker"
              defaultMessage="Image duplicate checker"
            />
          </h1>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            <FormattedMessage
              id="dupe_check.image_description"
              defaultMessage="Search for perceptually similar files."
            />
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {selectedImages.length > 0 && (
            <>
              <Button
                type="button"
                variant="secondary"
                onClick={() => setBulkEditOpen(true)}
              >
                <Pencil />
                <FormattedMessage id="actions.edit" defaultMessage="Edit" />
              </Button>
              <Button
                type="button"
                variant="destructive"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 />
                <FormattedMessage id="actions.delete" defaultMessage="Delete" />
              </Button>
            </>
          )}
        </div>
      </div>

      {missingPhashes > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <FormattedMessage
            id="dupe_check.missing_phash_warning"
            defaultMessage="{count, plural, one {# image is missing a perceptual hash.} other {# images are missing perceptual hashes.}}"
            values={{ count: missingPhashes }}
          />
        </div>
      )}

      <Card>
        <CardHeader className="gap-3 sm:grid-cols-[1fr_auto]">
          <div>
            <CardTitle>
              <FormattedMessage
                id="dupe_check.image_title"
                defaultMessage="Duplicate images"
              />
            </CardTitle>
            <CardDescription>
              <FormattedMessage
                id="dupe_check.found_sets"
                defaultMessage="{setCount, plural, one {# set of duplicates found.} other {# sets of duplicates found.}}"
                values={{ setCount: allGroups.length }}
              />
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2">
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
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">
                    <FormattedMessage
                      id="dupe_check.options.exact"
                      defaultMessage="Exact"
                    />
                  </SelectItem>
                  <SelectItem value="4">
                    <FormattedMessage
                      id="dupe_check.options.high"
                      defaultMessage="High"
                    />
                  </SelectItem>
                  <SelectItem value="8">
                    <FormattedMessage
                      id="dupe_check.options.medium"
                      defaultMessage="Medium"
                    />
                  </SelectItem>
                  <SelectItem value="10">
                    <FormattedMessage
                      id="dupe_check.options.low"
                      defaultMessage="Low"
                    />
                  </SelectItem>
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
        </CardHeader>

        <CardContent className="space-y-3">
          {allGroups.length === 0 ? (
            <Empty className="min-h-64">
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
            <>
              <DuplicateTable
                groups={pagedGroups}
                checkedImages={checkedImages}
                onCheckedChange={(id, checked) =>
                  setCheckedImages((prev) => ({ ...prev, [id]: checked }))
                }
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-sm text-muted-foreground">
                  {selectedImages.length > 0
                    ? intl.formatMessage(
                        {
                          id: "count_selected",
                          defaultMessage:
                            "{count, plural, one {# selected} other {# selected}}",
                        },
                        { count: selectedImages.length },
                      )
                    : null}
                </div>
                <div className="flex flex-wrap items-center gap-2">
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
                          allGroups.length > size ||
                          index === 0 ||
                          allGroups.length > PAGE_SIZES[index - 1]
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
                    <FormattedMessage id="actions.next" defaultMessage="Next" />
                  </Button>
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      <ImageBulkEditSheet
        open={bulkEditOpen}
        onOpenChange={setBulkEditOpen}
        items={selectedImages}
        onSaved={() => {
          setCheckedImages({});
          void duplicateQuery.refetch();
        }}
      />

      <DeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        entityCountLabel={intl.formatMessage(
          {
            id: "dialogs.delete_images_count",
            defaultMessage: "{count, plural, one {# image} other {# images}}",
          },
          { count: selectedImages.length },
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

function DuplicateTable({
  groups,
  checkedImages,
  onCheckedChange,
}: {
  groups: ImageGroup[];
  checkedImages: Record<string, boolean>;
  onCheckedChange: (id: string, checked: boolean) => void;
}) {
  const intl = useIntl();

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10" />
          <TableHead className="w-28" />
          <TableHead>
            <FormattedMessage id="details" defaultMessage="Details" />
          </TableHead>
          <TableHead>
            <FormattedMessage id="metadata" defaultMessage="Metadata" />
          </TableHead>
          <TableHead className="text-right">
            <FormattedMessage id="filesize" defaultMessage="File Size" />
          </TableHead>
          <TableHead className="text-right">
            <FormattedMessage id="resolution" defaultMessage="Resolution" />
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {groups.map((group, groupIndex) =>
          group.map((image, imageIndex) => {
            const file = primaryFile(image);
            const checked = checkedImages[image.id] ?? false;
            return (
              <TableRow
                key={image.id}
                className={cn(
                  imageIndex === 0 && groupIndex > 0 && "border-t-4",
                  checked && "bg-destructive/5",
                )}
              >
                <TableCell>
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
                </TableCell>
                <TableCell>
                  <Link to="/images/$imageId" params={{ imageId: image.id }}>
                    <img
                      src={image.paths.thumbnail ?? ""}
                      alt=""
                      className={cn(
                        "h-24 w-24 rounded-md border object-contain",
                        checked && "border-destructive ring-2 ring-destructive",
                      )}
                    />
                  </Link>
                </TableCell>
                <TableCell className="min-w-64 max-w-md whitespace-normal">
                  <Link
                    to="/images/$imageId"
                    params={{ imageId: image.id }}
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
                      <Badge variant="secondary">
                        <FormattedMessage
                          id="dupe_check.tags_count"
                          defaultMessage="{count, plural, one {# tag} other {# tags}}"
                          values={{ count: image.tags.length }}
                        />
                      </Badge>
                    )}
                    {image.performers.length > 0 && (
                      <Badge variant="secondary">
                        <FormattedMessage
                          id="dupe_check.performers_count"
                          defaultMessage="{count, plural, one {# performer} other {# performers}}"
                          values={{ count: image.performers.length }}
                        />
                      </Badge>
                    )}
                    {image.galleries.length > 0 && (
                      <Badge variant="secondary">
                        <FormattedMessage
                          id="dupe_check.galleries_count"
                          defaultMessage="{count, plural, one {# gallery} other {# galleries}}"
                          values={{ count: image.galleries.length }}
                        />
                      </Badge>
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
              </TableRow>
            );
          }),
        )}
      </TableBody>
    </Table>
  );
}

export const Route = createFileRoute("/image-duplicate-checker")({
  validateSearch: zodValidator(searchSchema),
  component: ImageDuplicateCheckerPage,
});
