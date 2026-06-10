/**
 * TanStack Form for creating / editing a scene marker.
 *
 * Three timestamp affordances per start / end input:
 *   1. Type `mm:ss.fff` (or seconds) directly into the `DurationInput`.
 *   2. Click the clock-icon "Use current time" button to grab the player's
 *      current playhead — wired via the page-level `getCurrentTime` getter.
 *   3. Drag the corresponding handle on the position slider; the page
 *      hands a `(boundary, time) => void` setter to this form via
 *      `registerBoundSetter` so player-side drag updates the form's
 *      field value.
 *
 * The form mirrors its `start` / `end` field values up to the parent on
 * every change via `onBoundsChange`. The parent feeds them into
 * `<ScenePlayer clipBoundsEdit={…}>` so the slider's handles always
 * track the in-flight form state.
 */
import { useEffect, useState } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { useLazyQuery } from "@apollo/client/react";
import { useEntityMutation } from "src/core/client";
import { useIntl } from "react-intl";
import { Save, RotateCcw, X } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { DurationInput } from "src/components/ui/duration-input";
import { Field, FieldLabel, FieldGroup } from "src/components/ui/field";
import {
  type EntityOption,
  EntityMultiSelect,
  EntitySingleSelect,
} from "src/components/forms/async-entity-select";

type SceneMarker = GQL.SceneMarkerDataFragment;

export interface MarkerFormBounds {
  start: number | null;
  end: number | null;
}

export type MarkerBoundary = "start" | "end";

interface MarkerFormValues {
  title: string;
  start: number | null;
  end: number | null;
  primary_tag: EntityOption | null;
  tags: EntityOption[];
}

function markerToFormValues(marker: SceneMarker | null): MarkerFormValues {
  return {
    title: marker?.title ?? "",
    start: marker?.seconds ?? null,
    end: marker?.end_seconds ?? null,
    primary_tag: marker?.primary_tag
      ? { id: marker.primary_tag.id, name: marker.primary_tag.name }
      : null,
    tags: (marker?.tags ?? []).map((t) => ({ id: t.id, name: t.name })),
  };
}

interface MarkerEditFormProps {
  sceneId: string;
  /** Existing marker for edit mode; null for create. */
  marker: SceneMarker | null;
  /**
   * Reads the player's current playhead in scene time. Used by the "use
   * current time" buttons inside the start / end inputs.
   */
  getCurrentTime?: () => number | undefined;
  /**
   * Emitted on every change to start / end. The page level pipes this
   * into `<ScenePlayer clipBoundsEdit={…}>` so the position slider's
   * handles render at the form's in-flight values.
   */
  onBoundsChange?: (bounds: MarkerFormBounds) => void;
  /**
   * Registers a setter so the page can write back into the form when the
   * user drags a handle on the position slider. Called once on mount.
   */
  registerBoundSetter?: (
    setter: (boundary: MarkerBoundary, time: number) => void,
  ) => void;
  /** Called after a successful save. */
  onSaved?: () => void;
  /** Called when the user clicks Cancel. */
  onCancel?: () => void;
}

export function MarkerEditForm({
  sceneId,
  marker,
  getCurrentTime,
  onBoundsChange,
  registerBoundSetter,
  onSaved,
  onCancel,
}: MarkerEditFormProps) {
  const intl = useIntl();
  const isEdit = marker != null;

  // useEntityMutation refetches every active query, which covers the scene's
  // `scene_markers` field (the marker editor is low-frequency, so the slight
  // overhead beats the toReference dance for a hand-rolled cache.modify).
  const [createMarker, { loading: creating }] = useEntityMutation(
    GQL.SceneMarkerCreateDocument,
  );
  const [updateMarker, { loading: updating }] = useEntityMutation(
    GQL.SceneMarkerUpdateDocument,
  );

  // ── Async-search options ──
  const [tagOptions, setTagOptions] = useState<EntityOption[]>([]);
  const [searchTags, { data: tagData, loading: tagLoading }] = useLazyQuery(
    GQL.FindTagsDocument,
  );
  useEffect(() => {
    if (tagData) {
      setTagOptions(
        tagData.findTags.tags.map((t) => ({ id: t.id, name: t.name })),
      );
    }
  }, [tagData]);

  // ── Form ──
  const form = useForm({
    defaultValues: markerToFormValues(marker),
    onSubmit: async ({ value, formApi }) => {
      // Validation gate: title, primary tag, and start are required for
      // both create and update. Form-level validation could also work but
      // keeping the guard inline avoids a second source of truth.
      if (
        !value.title.trim() ||
        !value.primary_tag ||
        value.start == null ||
        value.start < 0
      ) {
        return;
      }
      const variables = {
        title: value.title.trim(),
        seconds: value.start,
        end_seconds: value.end ?? null,
        scene_id: sceneId,
        primary_tag_id: value.primary_tag.id,
        tag_ids: value.tags.map((t) => t.id),
      };
      if (isEdit && marker) {
        await updateMarker({ variables: { id: marker.id, ...variables } });
      } else {
        await createMarker({ variables });
      }
      formApi.reset(value);
      onSaved?.();
    },
  });

  const busy = creating || updating;

  // ── Bidirectional bound binding ──
  // Form → parent: subscribe to start/end via the form store so changes
  // (typed input, clock-grab button, programmatic setter) propagate up.
  const start = useStore(form.store, (s) => s.values.start as number | null);
  const end = useStore(form.store, (s) => s.values.end as number | null);
  useEffect(() => {
    onBoundsChange?.({ start, end });
  }, [start, end, onBoundsChange]);

  // Parent → form: hand the parent a callback the player drag handler
  // calls to write a new value into the form. Registers once; the setter
  // closes over `form.setFieldValue` which is referentially stable.
  useEffect(() => {
    registerBoundSetter?.((boundary, time) => {
      form.setFieldValue(boundary, time);
    });
  }, [registerBoundSetter, form]);

  function captureCurrentTime(boundary: MarkerBoundary) {
    const t = getCurrentTime?.();
    if (t == null || !Number.isFinite(t)) return;
    // Truncate to ms precision — consistent with `DurationInput`'s
    // `[hh:]mm:ss.fff` formatting.
    form.setFieldValue(boundary, Math.round(t * 1000) / 1000);
  }

  return (
    <form
      className="min-w-0 overflow-x-hidden flex flex-col gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        form.handleSubmit();
      }}
    >
      <FieldGroup className="gap-3">
        {/* Title */}
        <form.Field name="title">
          {(field) => (
            <Field>
              <FieldLabel htmlFor={field.name}>
                {intl.formatMessage({ id: "title", defaultMessage: "Title" })}
              </FieldLabel>
              <Input
                id={field.name}
                value={field.state.value}
                onBlur={field.handleBlur}
                onChange={(e) => field.handleChange(e.target.value)}
                disabled={busy}
                placeholder={intl.formatMessage({
                  id: "marker_title_placeholder",
                  defaultMessage: "Optional — defaults to the primary tag name",
                })}
              />
            </Field>
          )}
        </form.Field>

        {/* Start / End */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <form.Field name="start">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "start_time",
                    defaultMessage: "Start",
                  })}
                </FieldLabel>
                <DurationInput
                  value={field.state.value}
                  setValue={(v) => field.handleChange(v)}
                  onReset={
                    getCurrentTime
                      ? () => captureCurrentTime("start")
                      : undefined
                  }
                  disabled={busy}
                  placeholder={intl.formatMessage({
                    id: "start",
                    defaultMessage: "Start",
                  })}
                />
              </Field>
            )}
          </form.Field>

          <form.Field name="end">
            {(field) => (
              <Field>
                <FieldLabel>
                  {intl.formatMessage({
                    id: "end_time",
                    defaultMessage: "End (optional)",
                  })}
                </FieldLabel>
                <DurationInput
                  value={field.state.value}
                  setValue={(v) => field.handleChange(v)}
                  onReset={
                    getCurrentTime ? () => captureCurrentTime("end") : undefined
                  }
                  disabled={busy}
                  placeholder={intl.formatMessage({
                    id: "end",
                    defaultMessage: "End",
                  })}
                />
              </Field>
            )}
          </form.Field>
        </div>

        {/* Primary tag */}
        <form.Field name="primary_tag">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({
                  id: "primary_tag",
                  defaultMessage: "Primary tag",
                })}
              </FieldLabel>
              <EntitySingleSelect
                value={field.state.value}
                onChange={field.handleChange}
                options={tagOptions}
                onSearch={(q) =>
                  searchTags({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={tagLoading}
                placeholder={intl.formatMessage({
                  id: "actions.search",
                  defaultMessage: "Search…",
                })}
                disabled={busy}
              />
            </Field>
          )}
        </form.Field>

        {/* Tags */}
        <form.Field name="tags">
          {(field) => (
            <Field>
              <FieldLabel>
                {intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
              </FieldLabel>
              <EntityMultiSelect
                value={field.state.value}
                onChange={field.handleChange}
                options={tagOptions}
                onSearch={(q) =>
                  searchTags({ variables: { filter: { q, per_page: 20 } } })
                }
                loading={tagLoading}
                placeholder={intl.formatMessage({
                  id: "actions.search",
                  defaultMessage: "Search…",
                })}
                disabled={busy}
              />
            </Field>
          )}
        </form.Field>
      </FieldGroup>

      {/* Action bar */}
      <div className="sticky bottom-0 z-10 flex items-center justify-between gap-2 border-t border-border bg-background/95 backdrop-blur-sm h-10 -mx-3 px-3">
        <form.Subscribe
          selector={(s) => ({
            isSubmitting: s.isSubmitting,
            isDirty: s.isDirty,
            values: s.values,
          })}
        >
          {({ isSubmitting, isDirty, values }) => {
            const valid =
              values.title.trim().length > 0 &&
              values.primary_tag != null &&
              values.start != null &&
              values.start >= 0 &&
              (values.end == null || values.end > values.start);
            return (
              <>
                <div className="flex items-center gap-2">
                  <Button
                    type="submit"
                    size="sm"
                    disabled={busy || isSubmitting || !valid || !isDirty}
                  >
                    <Save />
                    {intl.formatMessage({
                      id: "actions.save",
                      defaultMessage: "Save",
                    })}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={busy || !isDirty}
                    onClick={() => form.reset()}
                  >
                    <RotateCcw />
                    {intl.formatMessage({
                      id: "actions.discard",
                      defaultMessage: "Discard",
                    })}
                  </Button>
                </div>
                {onCancel && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={onCancel}
                    disabled={busy}
                  >
                    <X />
                    {intl.formatMessage({
                      id: "actions.cancel",
                      defaultMessage: "Cancel",
                    })}
                  </Button>
                )}
              </>
            );
          }}
        </form.Subscribe>
      </div>
    </form>
  );
}
