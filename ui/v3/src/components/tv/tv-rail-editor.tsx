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
import { ArrowUp, ArrowDown, GripVertical, Trash2, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
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
      <Field>
        <FieldLabel>
          <FormattedMessage
            id="tv.text.button_label"
            defaultMessage="Button label"
          />
        </FieldLabel>
        <Input
          value={action.label}
          onChange={(event) =>
            onChange({ ...action, label: event.target.value })
          }
          placeholder={msg(
            `tv.action.${action.kind}`,
            tvActionLabels[action.kind],
          )}
        />
      </Field>
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
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.marker_title"
                defaultMessage="Marker title"
              />
            </FieldLabel>
            <Input
              value={action.title}
              onChange={(event) =>
                onChange({ ...action, title: event.target.value })
              }
            />
          </Field>
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
          <Field>
            <FieldLabel>
              <FormattedMessage
                id="tv.text.duration_in_seconds_blank_for_implicit_end"
                defaultMessage="Duration in seconds (blank for implicit end)"
              />
            </FieldLabel>
            <Input
              type="number"
              min={0.1}
              value={action.duration ?? ""}
              onChange={(event) =>
                onChange({
                  ...action,
                  duration:
                    event.target.value === ""
                      ? null
                      : Number(event.target.value),
                })
              }
            />
          </Field>
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
  const [editing, setEditing] = useState(false);
  const essential =
    entry.type === "action" &&
    (entry.action.kind === "settings" || entry.action.kind === "visibility");
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
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition: transition,
      }}
      className="flex flex-col gap-3 rounded-lg border p-3"
    >
      <div className="flex flex-wrap items-center gap-1">
        <Button
          type="button"
          variant="ghost"
          size="icon-lg"
          aria-label={intl.formatMessage(
            { id: "tv.rail.drag", defaultMessage: "Drag {label}" },
            { label },
          )}
          {...attributes}
          {...listeners}
        >
          <GripVertical />
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="min-h-11 flex-1 justify-start"
          onClick={() => setEditing(!editing)}
        >
          {label}
          {entry.type === "folder" ? ` (${entry.actions.length})` : ""}
        </Button>
        <Switch
          checked={entry.pinned}
          aria-label={intl.formatMessage(
            { id: "tv.rail.pin", defaultMessage: "Pin {label}" },
            { label },
          )}
          onCheckedChange={(pinned) => update({ ...entry, pinned })}
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-lg"
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
          disabled={index === entries.length - 1}
          aria-label={intl.formatMessage(
            { id: "tv.rail.move_down", defaultMessage: "Move {label} down" },
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
      {editing &&
        (entry.type === "action" ? (
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
            <Field>
              <FieldLabel>
                <FormattedMessage
                  id="tv.text.folder_name"
                  defaultMessage="Folder name"
                />
              </FieldLabel>
              <Input
                value={entry.label}
                onChange={(event) =>
                  update({ ...entry, label: event.target.value })
                }
              />
            </Field>
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
        ))}
    </div>
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
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground">
        <FormattedMessage
          id="tv.text.drag_to_reorder_or_use_the_move_buttons_pinned_actions"
          defaultMessage="Drag to reorder, or use the move buttons. Pinned actions stay visible. Settings and visibility always remain available."
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
