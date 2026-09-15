import { FormattedMessage, useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { useState } from "react";
import {
  DndContext,
  PointerSensor,
  KeyboardSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  useSortable,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  ArrowUp,
  ArrowDown,
  ChevronDown,
  Folder,
  GripVertical,
  Pin,
  PinOff,
  Trash2,
  Plus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { SettingText } from "@/components/settings/setting-row";
import { Toggle } from "@/components/ui/toggle";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { Field, FieldLabel, FieldGroup } from "@/components/ui/field";
import { TvSelect } from "./tv-select";
import { TvTagPicker, TvTagsPicker } from "./tv-tag-picker";
import { tvActionLabels, tvIconLabels } from "./tv-action-labels";
import {
  createTvAction,
  railEntryId,
  tvActionKinds,
  tvIconIds,
  type TvAction,
  type TvActionKind,
  type TvRailEntry,
} from "@/core/tv/action-config";

// getRandomValues also works when Stash is served over a local HTTP address.
function createEntryId() {
  return Array.from(crypto.getRandomValues(new Uint32Array(4)), (value) =>
    value.toString(16).padStart(8, "0"),
  ).join("");
}

function ActionFields({
  action,
  onChange,
}: {
  action: TvAction;
  onChange: (next: TvAction) => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  return (
    <FieldGroup>
      <SettingText
        label={msg("tv.text.button_label", "Button label")}
        value={action.label}
        onChange={(label) => onChange({ ...action, label })}
        placeholder={msg(
          `tv.action.${action.kind}`,
          tvActionLabels[action.kind],
        )}
        inputClassName="w-full"
      />
      <Field>
        <FieldLabel>
          <FormattedMessage id="tv.text.icon" defaultMessage="Icon" />
        </FieldLabel>
        <TvSelect
          label={msg("tv.text.action_icon", "Action icon")}
          value={action.icon}
          options={tvIconIds.map((value) => ({
            value,
            label: intl.formatMessage(tvIconLabels[value]),
          }))}
          onChange={(icon) => onChange({ ...action, icon })}
        />
      </Field>
      {action.kind === "quick-tag" && (
        <>
          <Field>
            <FieldLabel>
              <FormattedMessage id="tv.text.tag" defaultMessage="Tag" />
            </FieldLabel>
            <TvTagPicker
              label={msg("tv.text.choose_a_tag", "Choose a tag")}
              value={
                action.tagId
                  ? {
                      id: action.tagId,
                      name: intl.formatMessage(
                        { id: "tv.rail.tag_id", defaultMessage: "Tag {id}" },
                        { id: action.tagId },
                      ),
                    }
                  : null
              }
              onChange={(tag) => onChange({ ...action, tagId: tag?.id ?? "" })}
            />
          </Field>
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.marker_tag_role"
                defaultMessage="Marker tag role"
              />
            </FieldLabel>
            <TvSelect
              label={msg("tv.text.marker_tag_role", "Marker tag role")}
              value={action.target}
              options={[
                {
                  value: "additional",
                  label: msg(
                    "tv.text.toggle_additional_tag",
                    "Toggle additional tag",
                  ),
                },
                {
                  value: "primary",
                  label: msg("tv.text.set_primary_tag", "Set primary tag"),
                },
              ]}
              onChange={(target) => onChange({ ...action, target })}
            />
          </Field>
        </>
      )}
      {action.kind === "quick-marker" && (
        <>
          <SettingText
            label={msg("tv.text.marker_title", "Marker title")}
            value={action.title}
            onChange={(title) => onChange({ ...action, title })}
            inputClassName="w-full"
          />
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.primary_tag"
                defaultMessage="Primary tag"
              />
            </FieldLabel>
            <TvTagPicker
              label={msg(
                "tv.text.choose_a_primary_tag",
                "Choose a primary tag",
              )}
              value={
                action.primaryTagId
                  ? {
                      id: action.primaryTagId,
                      name: intl.formatMessage(
                        { id: "tv.rail.tag_id", defaultMessage: "Tag {id}" },
                        { id: action.primaryTagId },
                      ),
                    }
                  : null
              }
              onChange={(tag) =>
                onChange({ ...action, primaryTagId: tag?.id ?? "" })
              }
            />
          </Field>
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.additional_tags"
                defaultMessage="Additional tags"
              />
            </FieldLabel>
            <TvTagsPicker
              label={msg("tv.text.additional_tags", "Additional tags")}
              value={action.tagIds.map((id) => ({
                id,
                name: intl.formatMessage(
                  { id: "tv.rail.tag_id", defaultMessage: "Tag {id}" },
                  { id },
                ),
              }))}
              onChange={(tags) =>
                onChange({ ...action, tagIds: tags.map((tag) => tag.id) })
              }
            />
          </Field>
          <SettingText
            label={msg(
              "tv.text.duration_in_seconds_blank_for_implicit_end",
              "Duration in seconds (blank for implicit end)",
            )}
            type="number"
            value={action.duration === null ? "" : String(action.duration)}
            onChange={(value) =>
              onChange({
                ...action,
                duration: value === "" ? null : Number(value),
              })
            }
            inputClassName="w-full"
          />
        </>
      )}
    </FieldGroup>
  );
}

function RailRow({
  entry,
  index,
  entries,
  onChange,
}: {
  entry: TvRailEntry;
  index: number;
  entries: TvRailEntry[];
  onChange: (entries: TvRailEntry[]) => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const id = railEntryId(entry);
  const { setNodeRef, transform, transition, attributes, listeners } =
    useSortable({ id });
  const essential =
    entry.type === "action" && entry.action.kind === "visibility";
  const update = (next: TvRailEntry) =>
    onChange(entries.map((item) => (railEntryId(item) === id ? next : item)));
  const label =
    entry.type === "folder"
      ? entry.label
      : entry.action.label ||
        msg(
          `tv.action.${entry.action.kind}`,
          tvActionLabels[entry.action.kind],
        );
  return (
    <Collapsible
      ref={setNodeRef}
      data-tv-rail-entry={id}
      style={{
        transform: CSS.Transform.toString(transform),
        transition: transition,
      }}
      className="rounded-lg border p-2"
    >
      <div className="grid gap-1 @xl:grid-cols-[minmax(0,1fr)_auto] @xl:items-center">
        <div className="flex min-w-0 items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-lg"
            className="size-11 touch-none text-muted-foreground"
            aria-label={intl.formatMessage(
              { id: "tv.rail.drag", defaultMessage: "Drag {label}" },
              { label },
            )}
            {...attributes}
            {...listeners}
          >
            <GripVertical />
          </Button>
          <CollapsibleTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                className="group/rail-trigger h-auto min-h-11 min-w-0 flex-1 justify-start gap-2 py-2 text-left whitespace-normal"
              />
            }
          >
            {entry.type === "folder" && <Folder data-icon="inline-start" />}
            <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">
              <span className="block">{label}</span>
              {entry.type === "folder" && (
                <span className="block text-xs font-normal text-muted-foreground">
                  <FormattedMessage
                    id="tv.rail.folder_count"
                    defaultMessage="Folder · {count, plural, one {# action} other {# actions}}"
                    values={{ count: entry.actions.length }}
                  />
                </span>
              )}
            </span>
            <ChevronDown
              data-icon="inline-end"
              className="text-muted-foreground transition-transform group-data-panel-open/rail-trigger:rotate-180"
            />
          </CollapsibleTrigger>
        </div>
        <div className="flex items-center justify-between gap-2">
          <Toggle
            type="button"
            variant="outline"
            className="h-11 min-w-24 shrink-0 px-3 text-muted-foreground data-pressed:border-primary/40 data-pressed:bg-primary/10 data-pressed:text-primary"
            pressed={entry.pinned}
            aria-label={intl.formatMessage(
              { id: "tv.rail.pin", defaultMessage: "Pin {label}" },
              { label },
            )}
            onPressedChange={(pinned) => update({ ...entry, pinned })}
          >
            {entry.pinned ? (
              <Pin data-icon="inline-start" fill="currentColor" />
            ) : (
              <PinOff data-icon="inline-start" />
            )}
            {entry.pinned
              ? msg("tv.rail.pinned", "Pinned")
              : msg("tv.rail.pin_action", "Pin")}
          </Toggle>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-lg"
              className="size-11"
              disabled={index === 0}
              aria-label={intl.formatMessage(
                { id: "tv.rail.move_up", defaultMessage: "Move {label} up" },
                { label },
              )}
              onClick={() => onChange(arrayMove(entries, index, index - 1))}
            >
              <ArrowUp />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-lg"
              className="size-11"
              disabled={index === entries.length - 1}
              aria-label={intl.formatMessage(
                {
                  id: "tv.rail.move_down",
                  defaultMessage: "Move {label} down",
                },
                { label },
              )}
              onClick={() => onChange(arrayMove(entries, index, index + 1))}
            >
              <ArrowDown />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-lg"
              className="size-11"
              disabled={essential}
              aria-label={intl.formatMessage(
                { id: "tv.rail.remove", defaultMessage: "Remove {label}" },
                { label },
              )}
              onClick={() =>
                onChange(entries.filter((item) => railEntryId(item) !== id))
              }
            >
              <Trash2 />
            </Button>
          </div>
        </div>
      </div>
      <CollapsibleContent className="flex flex-col gap-3 px-1 pt-3">
        {entry.type === "action" ? (
          <>
            <ActionFields
              action={entry.action}
              onChange={(action) => update({ ...entry, action })}
            />
            {!essential && (
              <TvSelect
                label={msg("tv.text.move_into_folder", "Move into folder")}
                value=""
                options={[
                  {
                    value: "",
                    label: msg(
                      "tv.text.move_into_folder_2",
                      "Move into folder…",
                    ),
                  },
                  ...entries
                    .filter((item) => item.type === "folder")
                    .map((item) => ({ value: item.id, label: item.label })),
                ]}
                onChange={(folderId) => {
                  if (!folderId) return;
                  onChange(
                    entries
                      .filter((item) => railEntryId(item) !== id)
                      .map((item) =>
                        item.type === "folder" && item.id === folderId
                          ? {
                              ...item,
                              actions: [...item.actions, entry.action],
                            }
                          : item,
                      ),
                  );
                }}
              />
            )}
          </>
        ) : (
          <FieldGroup>
            <SettingText
              label={msg("tv.text.folder_name", "Folder name")}
              value={entry.label}
              onChange={(label) => update({ ...entry, label })}
              inputClassName="w-full"
            />
            <Field>
              <FieldLabel>
                <FormattedMessage
                  id="tv.text.folder_icon"
                  defaultMessage="Folder icon"
                />
              </FieldLabel>
              <TvSelect
                label={msg("tv.text.folder_icon", "Folder icon")}
                value={entry.icon}
                options={tvIconIds.map((value) => ({
                  value,
                  label: intl.formatMessage(tvIconLabels[value]),
                }))}
                onChange={(icon) => update({ ...entry, icon })}
              />
            </Field>
            {entry.actions.map((action, childIndex) => (
              <div
                className="flex flex-col gap-2 rounded-lg border p-3"
                key={action.id}
              >
                <p>
                  {action.label ||
                    msg(
                      `tv.action.${action.kind}`,
                      tvActionLabels[action.kind],
                    )}
                </p>
                <ActionFields
                  action={action}
                  onChange={(next) =>
                    update({
                      ...entry,
                      actions: entry.actions.map((item) =>
                        item.id === action.id ? next : item,
                      ),
                    })
                  }
                />
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={childIndex === 0}
                    onClick={() =>
                      update({
                        ...entry,
                        actions: arrayMove(
                          entry.actions,
                          childIndex,
                          childIndex - 1,
                        ),
                      })
                    }
                  >
                    <FormattedMessage
                      id="tv.text.move_up"
                      defaultMessage="Move up"
                    />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={childIndex === entry.actions.length - 1}
                    onClick={() =>
                      update({
                        ...entry,
                        actions: arrayMove(
                          entry.actions,
                          childIndex,
                          childIndex + 1,
                        ),
                      })
                    }
                  >
                    <FormattedMessage
                      id="tv.text.move_down"
                      defaultMessage="Move down"
                    />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() =>
                      onChange([
                        ...entries.map((item) =>
                          railEntryId(item) === id
                            ? {
                                ...entry,
                                actions: entry.actions.filter(
                                  (item) => item.id !== action.id,
                                ),
                              }
                            : item,
                        ),
                        { type: "action", action, pinned: false },
                      ])
                    }
                  >
                    <FormattedMessage
                      id="tv.text.move_out"
                      defaultMessage="Move out"
                    />
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() =>
                      update({
                        ...entry,
                        actions: entry.actions.filter(
                          (item) => item.id !== action.id,
                        ),
                      })
                    }
                  >
                    <FormattedMessage
                      id="tv.text.remove"
                      defaultMessage="Remove"
                    />
                  </Button>
                </div>
              </div>
            ))}
          </FieldGroup>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

export default function TvRailEditor({
  value,
  onChange,
}: {
  value: TvRailEntry[];
  onChange: (entries: TvRailEntry[]) => void;
}) {
  const msg = useMsg();
  const [kind, setKind] = useState<TvActionKind>("quick-tag");
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );
  const present = new Set(
    value
      .flatMap((entry) =>
        entry.type === "folder" ? entry.actions : [entry.action],
      )
      .map((action) => action.kind),
  );
  const available = tvActionKinds.filter(
    (action) =>
      action === "quick-tag" ||
      action === "quick-marker" ||
      !present.has(action),
  );
  return (
    <div className="@container flex flex-col gap-4">
      <p className="text-muted-foreground">
        <FormattedMessage
          id="tv.rail.instructions"
          defaultMessage="Drag to reorder, or use the arrows. Select a name to edit. Pin actions or folders to the bottom bar."
        />
      </p>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={({ active, over }) => {
          if (!over || active.id === over.id) return;
          const from = value.findIndex(
            (entry) => railEntryId(entry) === active.id,
          );
          const to = value.findIndex((entry) => railEntryId(entry) === over.id);
          if (from >= 0 && to >= 0) onChange(arrayMove(value, from, to));
        }}
      >
        <SortableContext
          items={value.map(railEntryId)}
          strategy={verticalListSortingStrategy}
        >
          {value.map((entry, index) => (
            <RailRow
              key={railEntryId(entry)}
              entry={entry}
              index={index}
              entries={value}
              onChange={onChange}
            />
          ))}
        </SortableContext>
      </DndContext>
      <div className="flex flex-wrap items-center gap-2">
        <div className="min-w-40 flex-1">
          <TvSelect
            label={msg("tv.text.new_action", "New action")}
            value={kind}
            options={available.map((value) => ({
              value,
              label: msg(`tv.action.${value}`, tvActionLabels[value]),
            }))}
            onChange={setKind}
          />
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={!available.includes(kind)}
          onClick={() =>
            onChange([
              ...value,
              {
                type: "action",
                pinned: false,
                action: createTvAction(kind, createEntryId()),
              },
            ])
          }
        >
          <Plus data-icon="inline-start" />
          <FormattedMessage
            id="tv.text.add_action"
            defaultMessage="Add action"
          />
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            onChange([
              ...value,
              {
                type: "folder",
                id: createEntryId(),
                pinned: false,
                label: msg("tv.text.folder_2", "Folder"),
                icon: "default",
                actions: [],
              },
            ])
          }
        >
          <FormattedMessage
            id="tv.text.add_folder"
            defaultMessage="Add folder"
          />
        </Button>
      </div>
    </div>
  );
}
