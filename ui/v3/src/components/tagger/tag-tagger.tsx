import { Label } from "@/components/ui/label";
import type React from "react";
import { useId, useState, useCallback } from "react";
import { useIntl } from "react-intl";
import {
  useApolloClient,
  useMutation,
  useSubscription,
} from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext } from "src/hooks/config";

import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Checkbox } from "src/components/ui/checkbox";
import { Textarea } from "src/components/ui/textarea";
import { Spinner } from "src/components/ui/spinner";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "src/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "src/components/ui/dialog";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  EmptyDescription,
} from "src/components/ui/empty";
import { ServerOffIcon } from "lucide-react";

import { DEFAULT_EXCLUDED_TAG_FIELDS } from "./constants";

// ── types ─────────────────────────────────────────────────────────────────────

type TagItem = GQL.TagListDataFragment;

type ScrapedTag = GQL.ScrapeSingleTagQuery["scrapeSingleTag"][number];

interface ISaveDialogState {
  tag: TagItem;
  scraped: ScrapedTag;
  excludedFields: string[];
}

// ── helpers ───────────────────────────────────────────────────────────────────

function mergeStashIDs(
  existing: GQL.TagListDataFragment["stash_ids"],
  newID: GQL.StashIdInput,
): GQL.StashIdInput[] {
  const merged = existing
    .filter((s) => s.endpoint !== newID.endpoint)
    .map((s) => ({ endpoint: s.endpoint, stash_id: s.stash_id }));
  merged.push(newID);
  return merged;
}

function buildUpdateInput(
  tag: TagItem,
  scraped: ScrapedTag,
  excludedFields: string[],
  endpoint: string,
): GQL.TagUpdateInput {
  const input: GQL.TagUpdateInput = { id: tag.id };

  if (!excludedFields.includes("name") && scraped.name) {
    input.name = scraped.name;
  }
  if (!excludedFields.includes("description") && scraped.description != null) {
    input.description = scraped.description;
  }
  if (!excludedFields.includes("aliases") && scraped.alias_list != null) {
    input.aliases = scraped.alias_list;
  }
  // stash_id always included (not a user-toggled field)
  if (scraped.remote_site_id) {
    input.stash_ids = mergeStashIDs(tag.stash_ids, {
      endpoint,
      stash_id: scraped.remote_site_id,
    });
  }

  return input;
}

function alreadyTagged(tag: TagItem, endpoint: string): boolean {
  return tag.stash_ids.some((s) => s.endpoint === endpoint);
}

// ── TagSaveDialog ──────────────────────────────────────────────────────────────

interface ITagSaveDialogProps {
  state: ISaveDialogState | null;
  onSave: (input: GQL.TagUpdateInput, createParent: boolean) => void;
  onClose: () => void;
  endpoint: string;
}

const TagSaveDialog: React.FC<ITagSaveDialogProps> = ({
  state,
  onSave,
  onClose,
  endpoint,
}) => {
  const intl = useIntl();
  const controlId = useId();
  const [excluded, setExcluded] = useState<string[]>(
    () => state?.excludedFields ?? DEFAULT_EXCLUDED_TAG_FIELDS,
  );
  const [createParent, setCreateParent] = useState(true);

  if (!state) return null;

  const { tag, scraped } = state;

  function toggle(field: string) {
    setExcluded((prev) =>
      prev.includes(field) ? prev.filter((f) => f !== field) : [...prev, field],
    );
  }

  const availableFields: Array<{
    field: string;
    label: string;
    value: string | null;
  }> = [
    {
      field: "name",
      label: intl.formatMessage({ id: "name", defaultMessage: "Name" }),
      value: scraped.name,
    },
    {
      field: "description",
      label: intl.formatMessage({
        id: "description",
        defaultMessage: "Description",
      }),
      value: scraped.description ?? null,
    },
    {
      field: "aliases",
      label: intl.formatMessage({ id: "aliases", defaultMessage: "Aliases" }),
      value: scraped.alias_list?.join(", ") ?? null,
    },
  ];

  const parentTag = scraped.parent;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage(
              { id: "tagger.save_tag", defaultMessage: "Save {name}" },
              { name: tag.name },
            )}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-3 py-2">
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage({
              id: "tagger.select_fields",
              defaultMessage: "Select which fields to update:",
            })}
          </p>

          {availableFields.map(({ field, label, value }) =>
            value != null ? (
              <Label
                htmlFor={`${controlId}-${field}`}
                key={field}
                className="flex cursor-pointer items-start gap-2.5 text-sm"
              >
                <Checkbox
                  id={`${controlId}-${field}`}
                  checked={!excluded.includes(field)}
                  onCheckedChange={() => toggle(field)}
                  className="mt-0.5"
                />
                <span>
                  <span className="font-medium">{label}:</span>{" "}
                  <span className="text-muted-foreground">{value}</span>
                </span>
              </Label>
            ) : null,
          )}

          {parentTag && (
            <Label
              htmlFor={`${controlId}-parent-tags`}
              className="flex cursor-pointer items-start gap-2.5 text-sm"
            >
              <Checkbox
                id={`${controlId}-parent-tags`}
                checked={!excluded.includes("parent_tags")}
                onCheckedChange={() => toggle("parent_tags")}
                className="mt-0.5"
              />
              <span>
                <span className="font-medium">
                  {intl.formatMessage({
                    id: "parent_tag",
                    defaultMessage: "Parent tag",
                  })}
                  :
                </span>{" "}
                <span className="text-muted-foreground">
                  {parentTag.stored_id ? (
                    parentTag.name
                  ) : (
                    <>
                      {parentTag.name}
                      <span className="ml-1 text-xs text-yellow-600">
                        (
                        {intl.formatMessage({
                          id: "tagger.will_create",
                          defaultMessage: "will create",
                        })}
                        )
                      </span>
                    </>
                  )}
                </span>
              </span>
            </Label>
          )}

          {parentTag &&
            !parentTag.stored_id &&
            !excluded.includes("parent_tags") && (
              <Label
                htmlFor={`${controlId}-create-parent`}
                className="flex cursor-pointer items-center gap-2.5 text-sm pl-6"
              >
                <Checkbox
                  id={`${controlId}-create-parent`}
                  checked={createParent}
                  onCheckedChange={(c) => setCreateParent(c === true)}
                />
                {intl.formatMessage({
                  id: "tagger.create_parent_tag",
                  defaultMessage: "Create missing parent tag",
                })}
              </Label>
            )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            onClick={() => {
              const input = buildUpdateInput(tag, scraped, excluded, endpoint);
              onSave(input, !excluded.includes("parent_tags") && createParent);
            }}
          >
            {intl.formatMessage({ id: "actions.save", defaultMessage: "Save" })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

// ── BatchAddDialog ──────────────────────────────────────────────────────────────

interface IBatchAddDialogProps {
  open: boolean;
  endpoint: string;
  onClose: () => void;
  onSubmit: (input: GQL.StashBoxBatchTagInput) => void;
}

const BatchAddDialog: React.FC<IBatchAddDialogProps> = ({
  open,
  endpoint,
  onClose,
  onSubmit,
}) => {
  const intl = useIntl();
  const controlId = useId();
  const [names, setNames] = useState("");
  const [refresh, setRefresh] = useState(false);
  const [createParent, setCreateParent] = useState(true);

  function handleSubmit() {
    const nameList = names
      .split("\n")
      .map((n) => n.trim())
      .filter(Boolean);
    onSubmit({
      stash_box_endpoint: endpoint,
      names: nameList,
      refresh,
      createParent,
    });
    onClose();
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "tagger.batch_add",
              defaultMessage: "Batch Add Tags",
            })}
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-2">
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage({
              id: "tagger.batch_add_description",
              defaultMessage:
                "Enter tag names (one per line) to search for and add from stash-box:",
            })}
          </p>
          <Textarea
            rows={8}
            value={names}
            onChange={(e) => setNames(e.target.value)}
            placeholder={intl.formatMessage({
              id: "tagger.batch_add_placeholder",
              defaultMessage: "Tag name\nAnother tag\n...",
            })}
          />
          <Label
            htmlFor={`${controlId}-refresh`}
            className="flex cursor-pointer items-center gap-2.5 text-sm"
          >
            <Checkbox
              id={`${controlId}-refresh`}
              checked={refresh}
              onCheckedChange={(c) => setRefresh(c === true)}
            />
            {intl.formatMessage({
              id: "tagger.refresh_existing",
              defaultMessage: "Refresh already tagged items",
            })}
          </Label>
          <Label
            htmlFor={`${controlId}-create-parent`}
            className="flex cursor-pointer items-center gap-2.5 text-sm"
          >
            <Checkbox
              id={`${controlId}-create-parent`}
              checked={createParent}
              onCheckedChange={(c) => setCreateParent(c === true)}
            />
            {intl.formatMessage({
              id: "tagger.create_parent_tag",
              defaultMessage: "Create missing parent tag",
            })}
          </Label>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button onClick={handleSubmit} disabled={!names.trim()}>
            {intl.formatMessage({ id: "actions.add", defaultMessage: "Add" })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

// ── TagRow ─────────────────────────────────────────────────────────────────────

type RowStatus =
  | { type: "idle" }
  | { type: "searching" }
  | { type: "results"; results: ScrapedTag[] }
  | { type: "saving" }
  | { type: "saved" }
  | { type: "error"; message: string };

interface ITagRowProps {
  tag: TagItem;
  endpoint: string;
  onOpenSaveDialog: (tag: TagItem, scraped: ScrapedTag) => void;
}

const TagRow: React.FC<ITagRowProps> = ({
  tag,
  endpoint,
  onOpenSaveDialog,
}) => {
  const intl = useIntl();
  const client = useApolloClient();
  const [query, setQuery] = useState(tag.name);
  const [status, setStatus] = useState<RowStatus>({ type: "idle" });

  const tagged = alreadyTagged(tag, endpoint);

  async function handleSearch() {
    if (!query.trim()) return;
    setStatus({ type: "searching" });
    try {
      const { data } = await client.query<
        GQL.ScrapeSingleTagQuery,
        GQL.ScrapeSingleTagQueryVariables
      >({
        query: GQL.ScrapeSingleTagDocument,
        variables: {
          source: { stash_box_endpoint: endpoint },
          input: { query: query.trim() },
        },
        fetchPolicy: "no-cache",
      });
      const results = data?.scrapeSingleTag ?? [];
      setStatus({ type: "results", results });
    } catch (e) {
      setStatus({
        type: "error",
        message: e instanceof Error ? e.message : String(e),
      });
    }
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") handleSearch();
  }

  const results = status.type === "results" ? status.results : [];

  return (
    <div className="flex flex-col gap-2 border-b border-border pb-3 last:border-0">
      <div className="flex items-center gap-2">
        {/* Tag image */}
        {tag.image_path ? (
          <img
            src={tag.image_path}
            alt={tag.name}
            className="size-10 rounded-sm object-cover shrink-0"
          />
        ) : (
          <div className="size-10 rounded-sm bg-muted shrink-0" />
        )}

        {/* Name + stash IDs */}
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium truncate">{tag.name}</p>
          {tagged && (
            <p className="text-xs text-muted-foreground">
              {intl.formatMessage({
                id: "tagger.already_tagged",
                defaultMessage: "Already tagged",
              })}
            </p>
          )}
        </div>

        {/* Search input */}
        <Input
          className="w-40 h-8 text-sm"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={intl.formatMessage({
            id: "actions.search",
            defaultMessage: "Search",
          })}
        />
        <Button
          variant="outline"
          size="sm"
          onClick={handleSearch}
          disabled={status.type === "searching"}
        >
          {status.type === "searching" ? (
            <Spinner />
          ) : (
            intl.formatMessage({
              id: "actions.search",
              defaultMessage: "Search",
            })
          )}
        </Button>
      </div>

      {/* Results */}
      {status.type === "results" && results.length === 0 && (
        <p className="text-xs text-muted-foreground pl-12">
          {intl.formatMessage({
            id: "tagger.no_results",
            defaultMessage: "No results found",
          })}
        </p>
      )}

      {results.length > 0 && (
        <div className="flex flex-col gap-1 pl-12">
          {results.map((r, idx) => (
            <div
              key={idx}
              className="flex items-center justify-between rounded-md border border-border bg-muted/30 px-3 py-2 text-sm"
            >
              <div className="min-w-0">
                <span className="font-medium">{r.name}</span>
                {r.description && (
                  <span className="ml-2 text-muted-foreground text-xs truncate">
                    {r.description}
                  </span>
                )}
                {r.parent && (
                  <span className="ml-2 text-xs text-muted-foreground">
                    /{r.parent.name}
                  </span>
                )}
              </div>
              <Button
                variant="outline"
                size="sm"
                className="ml-2 shrink-0"
                onClick={() => onOpenSaveDialog(tag, r)}
              >
                {intl.formatMessage({
                  id: "actions.save",
                  defaultMessage: "Save",
                })}
              </Button>
            </div>
          ))}
        </div>
      )}

      {status.type === "error" && (
        <p className="text-xs text-destructive pl-12">{status.message}</p>
      )}

      {status.type === "saved" && (
        <p className="text-xs text-green-600 pl-12">
          {intl.formatMessage({ id: "tagger.saved", defaultMessage: "Saved" })}
        </p>
      )}
    </div>
  );
};

// ── TagTagger (main) ───────────────────────────────────────────────────────────

interface ITagTaggerProps {
  tags: TagItem[];
}

export const TagTagger: React.FC<ITagTaggerProps> = ({ tags }) => {
  const intl = useIntl();
  const client = useApolloClient();
  const { configuration } = useConfigurationContext();

  const stashBoxes = configuration.general.stashBoxes ?? [];
  const [endpoint, setEndpoint] = useState<string>(
    () => stashBoxes[0]?.endpoint ?? "",
  );

  const [saveDialogState, setSaveDialogState] =
    useState<ISaveDialogState | null>(null);
  const [batchAddOpen, setBatchAddOpen] = useState(false);
  const [batchJobId, setBatchJobId] = useState<string | null>(null);
  const [batchProgress, setBatchProgress] = useState<number | null>(null);
  const [batchError, setBatchError] = useState<string | null>(null);

  const [tagUpdate] = useMutation(GQL.TagUpdateDocument);
  const [tagCreate] = useMutation(GQL.TagCreateDocument);
  const [batchTagTag] = useMutation(GQL.StashBoxBatchTagTagDocument);

  // Track batch job progress
  useSubscription(GQL.JobsSubscribeDocument, {
    skip: batchJobId === null,
    onData: ({ data: { data } }) => {
      const update = data?.jobsSubscribe;
      if (!update || update.job.id !== batchJobId) return;
      if (update.type === GQL.JobStatusUpdateType.Remove) {
        setBatchJobId(null);
        setBatchProgress(null);
      } else if (update.type === GQL.JobStatusUpdateType.Update) {
        setBatchProgress(update.job.progress ?? null);
        if (update.job.error) setBatchError(update.job.error);
      }
    },
  });

  const openSaveDialog = useCallback((tag: TagItem, scraped: ScrapedTag) => {
    setSaveDialogState({
      tag,
      scraped,
      excludedFields: DEFAULT_EXCLUDED_TAG_FIELDS,
    });
  }, []);

  async function handleSave(input: GQL.TagUpdateInput, createParent: boolean) {
    const state = saveDialogState;
    if (!state) return;
    setSaveDialogState(null);

    try {
      // Create missing parent if needed
      if (
        createParent &&
        state.scraped.parent &&
        !state.scraped.parent.stored_id
      ) {
        const parentResult = await tagCreate({
          variables: {
            input: {
              name: state.scraped.parent.name,
              description: state.scraped.parent.description ?? undefined,
            },
          },
        });
        const parentId = parentResult.data?.tagCreate?.id;
        if (parentId) {
          const existing =
            input.parent_ids ?? state.tag.parents.map((p) => p.id);
          input.parent_ids = [...existing, parentId];
        }
      } else if (
        !createParent &&
        state.scraped.parent?.stored_id &&
        input.parent_ids === undefined
      ) {
        // Link to existing parent
        const existing = input.parent_ids ?? state.tag.parents.map((p) => p.id);
        if (!existing.includes(state.scraped.parent.stored_id)) {
          input.parent_ids = [...existing, state.scraped.parent.stored_id];
        }
      }

      await tagUpdate({ variables: { input } });

      // Evict from Apollo cache so list re-fetches
      client.cache.evict({
        id: client.cache.identify({ __typename: "Tag", id: input.id }),
      });
      client.cache.gc();
    } catch {
      // errors surface in Apollo's error state; ignore here
    }
  }

  async function handleBatchAdd(input: GQL.StashBoxBatchTagInput) {
    setBatchError(null);
    try {
      const result = await batchTagTag({ variables: { input } });
      const jobId = result.data?.stashBoxBatchTagTag;
      if (jobId) setBatchJobId(jobId);
    } catch (e) {
      setBatchError(e instanceof Error ? e.message : String(e));
    }
  }

  if (stashBoxes.length === 0) {
    return (
      <Empty className="border border-dashed border-border rounded-lg m-4">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ServerOffIcon />
          </EmptyMedia>
          <EmptyTitle>
            {intl.formatMessage({
              id: "tagger.no_stash_boxes_title",
              defaultMessage: "No metadata providers",
            })}
          </EmptyTitle>
          <EmptyDescription>
            {intl.formatMessage({
              id: "tagger.no_stash_boxes",
              defaultMessage:
                "No stash-box instances configured. Add one in Settings → Metadata Providers.",
            })}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <div className="flex flex-col gap-4 p-4">
      {/* Header toolbar */}
      <div className="flex flex-wrap items-center gap-2">
        {stashBoxes.length > 1 ? (
          <Select
            value={endpoint}
            onValueChange={(v) => {
              if (v !== null) setEndpoint(v);
            }}
          >
            <SelectTrigger size="sm" className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {stashBoxes.map((box) => (
                <SelectItem key={box.endpoint} value={box.endpoint}>
                  {box.name || box.endpoint}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : (
          <span className="text-sm text-muted-foreground">
            {stashBoxes[0]?.name || stashBoxes[0]?.endpoint}
          </span>
        )}

        <Button
          variant="outline"
          size="sm"
          onClick={() => setBatchAddOpen(true)}
          disabled={!endpoint}
        >
          {intl.formatMessage({
            id: "tagger.batch_add",
            defaultMessage: "Batch Add",
          })}
        </Button>

        {batchJobId !== null && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Spinner />
            {batchProgress != null ? (
              <span>{Math.round(batchProgress * 100)}%</span>
            ) : (
              <span>
                {intl.formatMessage({
                  id: "tagger.running",
                  defaultMessage: "Running…",
                })}
              </span>
            )}
          </div>
        )}

        {batchError && (
          <span className="text-sm text-destructive">{batchError}</span>
        )}
      </div>

      {/* Tag rows */}
      <div className="flex flex-col gap-3">
        {tags.map((tag) => (
          <TagRow
            key={tag.id}
            tag={tag}
            endpoint={endpoint}
            onOpenSaveDialog={openSaveDialog}
          />
        ))}
      </div>

      {/* Dialogs */}
      <TagSaveDialog
        state={saveDialogState}
        endpoint={endpoint}
        onSave={handleSave}
        onClose={() => setSaveDialogState(null)}
      />

      <BatchAddDialog
        open={batchAddOpen}
        endpoint={endpoint}
        onClose={() => setBatchAddOpen(false)}
        onSubmit={handleBatchAdd}
      />
    </div>
  );
};
