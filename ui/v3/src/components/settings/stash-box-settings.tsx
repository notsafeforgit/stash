/**
 * Stash-box endpoints editor for the Metadata Providers settings page.
 * Each box has a display name, GraphQL endpoint, API key and an optional
 * request rate limit. Edits go through a dialog with a "test credentials"
 * action backed by the ValidateStashBox query.
 */
import { useState } from "react";
import { useMsg } from "src/hooks/message";
import { useLazyQuery } from "@apollo/client/react";
import { Pencil, Trash2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "src/components/ui/field";
import { Spinner } from "src/components/ui/spinner";

export interface StashBoxDraft {
  name: string;
  endpoint: string;
  api_key: string;
  max_requests_per_minute: number;
}

function StashBoxDialog({
  open,
  initial,
  onCancel,
  onSave,
}: {
  open: boolean;
  initial: StashBoxDraft;
  onCancel: () => void;
  onSave: (draft: StashBoxDraft) => void;
}) {
  const [draft, setDraft] = useState(initial);

  // Re-seed the draft whenever a different box is opened for editing.
  const [syncedInitial, setSyncedInitial] = useState(initial);
  if (syncedInitial !== initial) {
    setSyncedInitial(initial);
    setDraft(initial);
  }

  const [validate, { data: validateData, loading: validating }] = useLazyQuery(
    GQL.ValidateStashBoxDocument,
    { fetchPolicy: "network-only" },
  );

  const validation = validateData?.validateStashBoxCredentials;

  function set(patch: Partial<StashBoxDraft>) {
    setDraft({ ...draft, ...patch });
  }

  const msg = useMsg();

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {msg("config.stashbox.title", "Stash-box endpoints")}
          </DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="stashbox-name">
              {msg("config.stashbox.name", "Name")}
            </FieldLabel>
            <Input
              id="stashbox-name"
              value={draft.name}
              onChange={(e) => set({ name: e.currentTarget.value })}
            />
            <FieldDescription>
              {msg("config.stashbox.name_desc", "Display name for this box.")}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="stashbox-endpoint">
              {msg("config.stashbox.endpoint", "Endpoint")}
            </FieldLabel>
            <Input
              id="stashbox-endpoint"
              placeholder="https://stashdb.org/graphql"
              value={draft.endpoint}
              onChange={(e) => set({ endpoint: e.currentTarget.value })}
            />
            <FieldDescription>
              {msg(
                "config.stashbox.graphql_endpoint",
                "GraphQL endpoint URL of the stash-box instance.",
              )}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="stashbox-apikey">
              {msg("config.general.auth.api_key", "API key")}
            </FieldLabel>
            <Input
              id="stashbox-apikey"
              value={draft.api_key}
              onChange={(e) => set({ api_key: e.currentTarget.value })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="stashbox-rate-limit">
              {msg(
                "config.stashbox.max_requests_per_minute",
                "Max requests per minute",
              )}
            </FieldLabel>
            <Input
              id="stashbox-rate-limit"
              type="number"
              inputMode="numeric"
              className="w-28"
              value={String(draft.max_requests_per_minute)}
              onChange={(e) => {
                const parsed = Number(e.currentTarget.value);
                set({
                  max_requests_per_minute: Number.isFinite(parsed) ? parsed : 0,
                });
              }}
            />
            <FieldDescription>
              {msg(
                "config.stashbox.max_requests_per_minute_desc",
                "Rate limit for requests to this box. 0 to disable.",
              )}
            </FieldDescription>
          </Field>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={!draft.endpoint || validating}
              onClick={() =>
                void validate({
                  variables: {
                    input: {
                      name: draft.name,
                      endpoint: draft.endpoint,
                      api_key: draft.api_key,
                    },
                  },
                })
              }
            >
              {msg("config.stashbox.test_credentials", "Test credentials")}
            </Button>
            {validating && <Spinner className="size-4" />}
            {!validating && validation && (
              <span
                className={
                  validation.valid
                    ? "text-sm text-emerald-500"
                    : "text-sm text-destructive"
                }
              >
                {validation.status}
              </span>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancel}>
            {msg("actions.cancel", "Cancel")}
          </Button>
          <Button
            type="button"
            disabled={!draft.endpoint}
            onClick={() => onSave(draft)}
          >
            {msg("actions.save", "Save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const EMPTY_DRAFT: StashBoxDraft = {
  name: "",
  endpoint: "",
  api_key: "",
  max_requests_per_minute: 0,
};

export function StashBoxSettings({
  boxes,
  onChange,
}: {
  boxes: StashBoxDraft[];
  onChange: (next: StashBoxDraft[]) => void;
}) {
  // null = closed; -1 = adding; otherwise index being edited
  const [editIndex, setEditIndex] = useState<number | null>(null);

  const msg = useMsg();

  return (
    <div className="space-y-3">
      {boxes.length === 0 && (
        <p className="text-sm text-muted-foreground">
          {msg(
            "config.stashbox.no_boxes",
            "No stash-box endpoints configured.",
          )}
        </p>
      )}
      {boxes.map((box, i) => (
        <div
          key={box.endpoint}
          className="flex items-center justify-between gap-2 rounded-lg border p-3"
        >
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">
              {box.name || box.endpoint}
            </div>
            <div className="truncate text-xs text-muted-foreground">
              {box.endpoint}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={msg("actions.edit", "Edit")}
              onClick={() => setEditIndex(i)}
            >
              <Pencil className="size-4" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={msg("actions.delete", "Delete")}
              onClick={() => onChange(boxes.filter((_, j) => j !== i))}
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        </div>
      ))}
      <Button type="button" variant="outline" onClick={() => setEditIndex(-1)}>
        {msg("config.stashbox.add_instance", "Add stash-box instance")}…
      </Button>

      <StashBoxDialog
        open={editIndex !== null}
        initial={
          editIndex !== null && editIndex >= 0 ? boxes[editIndex] : EMPTY_DRAFT
        }
        onCancel={() => setEditIndex(null)}
        onSave={(draft) => {
          if (editIndex === null) return;
          if (editIndex >= 0) {
            onChange(boxes.map((b, i) => (i === editIndex ? draft : b)));
          } else {
            onChange([...boxes, draft]);
          }
          setEditIndex(null);
        }}
      />
    </div>
  );
}
