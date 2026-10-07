import { useId } from "react";
import { useMsg } from "@/hooks/message";
import type {
  PostMergeInput,
  PostMergePost,
} from "@/core/native-archive/post-consolidation-schema";
import { Field, FieldLabel, FieldDescription } from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  MergeEntity,
  MergeList,
  attachmentKey,
  useMergeLabels,
} from "./shared";

type Props<K extends keyof PostMergeInput> = {
  posts: PostMergePost[];
  value: PostMergeInput[K];
  disabled: boolean;
  onChange: (value: PostMergeInput[K]) => void;
};

function Choice({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <Field data-disabled={disabled}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select
        items={options}
        value={value}
        disabled={disabled}
        onValueChange={(value) => {
          if (value !== null) onChange(value);
        }}
      >
        <SelectTrigger id={id} className="w-full min-w-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </Field>
  );
}

export function MergeSelectionChoice({
  posts,
  value,
  disabled,
  onChange,
}: Props<"selection">) {
  const msg = useMsg();
  const labels = useMergeLabels();
  const mode = value?.mode ?? "preserve";
  const id = useId();
  const candidates = posts.filter(
    (post) =>
      post.selection &&
      (mode !== "combine" || post.selection.mode !== "disabled"),
  );
  const chooseMode = (next: string) => {
    if (next === "preserve") onChange(undefined);
    else if (next === "disabled") onChange({ mode: "disabled" });
    else if (next === "choose" || next === "combine") {
      const selected =
        posts.find(
          (p) =>
            p.selection &&
            (next !== "combine" || p.selection.mode !== "disabled") &&
            p.selection.decision_uuid === value?.decision_uuid,
        ) ??
        posts.find(
          (p) =>
            p.selection &&
            (next !== "combine" || p.selection.mode !== "disabled"),
        );
      if (selected?.selection)
        onChange({
          mode: next,
          decision_uuid: selected.selection.decision_uuid,
        });
    }
  };
  return (
    <>
      <Field data-disabled={disabled}>
        <FieldLabel id={id}>
          {msg("post_merge.source_list", "Resulting source list")}
        </FieldLabel>
        <ToggleGroup
          multiple={false}
          variant="outline"
          value={[mode]}
          disabled={disabled}
          aria-labelledby={id}
          className="flex-wrap"
          onValueChange={(values) => {
            if (values[0]) chooseMode(values[0]);
          }}
        >
          <ToggleGroupItem value="preserve">
            {msg("post_merge.preserve", "Preserve agreeing choices")}
          </ToggleGroupItem>
          <ToggleGroupItem
            value="choose"
            disabled={!posts.some((p) => p.selection)}
          >
            {msg("post_merge.choose_list", "Choose one list")}
          </ToggleGroupItem>
          <ToggleGroupItem
            value="combine"
            disabled={
              !posts.some((p) => p.selection && p.selection.mode !== "disabled")
            }
          >
            {msg("post_merge.combine", "Combine compatible lists")}
          </ToggleGroupItem>
          <ToggleGroupItem value="disabled">
            {labels.selection.disabled}
          </ToggleGroupItem>
        </ToggleGroup>
        <FieldDescription>
          {msg(
            "post_merge.list_help",
            "Preserve keeps matching choices. Combining keeps the primary list's text and order, and fills compatible positions from the other lists. Conflicting positions need one selected list.",
          )}
        </FieldDescription>
      </Field>
      {(mode === "choose" || mode === "combine") && (
        <Choice
          label={
            mode === "combine"
              ? msg("post_merge.primary_list", "Primary source list")
              : msg("post_merge.selected_list", "Selected source list")
          }
          value={value?.decision_uuid ?? ""}
          disabled={disabled}
          options={candidates.flatMap((post) =>
            post.selection
              ? [
                  {
                    value: post.selection.decision_uuid,
                    label: `${labels.name(post)} · ${labels.selection[post.selection.mode]}`,
                  },
                ]
              : [],
          )}
          onChange={(decision_uuid) => onChange({ mode, decision_uuid })}
        />
      )}
    </>
  );
}

export function MergeGalleryChoice({
  posts,
  value,
  disabled,
  onChange,
}: Props<"gallery">) {
  const msg = useMsg();
  const galleries = [
    ...new Map(
      posts.flatMap((post) =>
        post.album?.gallery
          ? [[post.album.gallery.uuid, post.album.gallery] as const]
          : [],
      ),
    ).values(),
  ];
  const options = [
    {
      value: "preserve",
      label: msg("post_merge.preserve", "Preserve agreeing choices"),
    },
    { value: "disabled", label: msg("association_review.disable", "Disable") },
    ...galleries
      .filter((gallery) => gallery.state === "active")
      .map((gallery) => ({
        value: gallery.uuid,
        label: `${gallery.title || msg("gallery", "Gallery")} #${gallery.local_id}`,
      })),
  ];
  return (
    <Choice
      label={msg("post_merge.gallery", "Resulting gallery association")}
      value={
        value?.state === "disabled"
          ? "disabled"
          : (value?.gallery_uuid ?? "preserve")
      }
      options={options}
      disabled={disabled}
      onChange={(next) =>
        onChange(
          next === "preserve"
            ? undefined
            : next === "disabled"
              ? { state: "disabled" }
              : { state: "linked", gallery_uuid: next },
        )
      }
    />
  );
}

export function MergeMediaChoices({
  posts,
  value,
  disabled,
  onChange,
}: Props<"media">) {
  const msg = useMsg();
  const labels = useMergeLabels();
  const id = useId();
  const groups = new Map<string, PostMergePost["media_choices"]>();
  for (const post of posts)
    for (const item of post.media_choices)
      groups.set(item.media.uuid, [
        ...(groups.get(item.media.uuid) ?? []),
        item,
      ]);
  return (
    <MergeList items={[...groups.entries()]} rowKey={([key]) => key}>
      {([, items]) => {
        const first = items[0];
        if (!first) return null;
        const media = first.media,
          chosen =
            value.find((v) => v.media_uuid === media.uuid)?.state ?? "preserve";
        const labelId = `${id}-${media.uuid}`;
        return (
          <Field data-disabled={disabled}>
            <FieldLabel id={labelId}>
              {msg("post_merge.media_link", "Post link for this media")}
            </FieldLabel>
            <MergeEntity item={media} />
            <ToggleGroup
              multiple={false}
              variant="outline"
              aria-labelledby={labelId}
              value={[chosen]}
              disabled={disabled}
              className="flex-wrap"
              onValueChange={(next) => {
                const state = next[0];
                if (state === "preserve")
                  onChange(value.filter((v) => v.media_uuid !== media.uuid));
                else if (
                  state === "linked" ||
                  state === "unlinked" ||
                  state === "undecided"
                )
                  onChange([
                    ...value.filter((v) => v.media_uuid !== media.uuid),
                    { media_uuid: media.uuid, state },
                  ]);
              }}
            >
              <ToggleGroupItem value="preserve">
                {msg("post_merge.preserve", "Preserve agreeing choices")}
              </ToggleGroupItem>
              <ToggleGroupItem
                value="linked"
                disabled={
                  media.state === "deleted" &&
                  !items.some((item) => item.decision.state === "linked")
                }
              >
                {labels.state.linked}
              </ToggleGroupItem>
              <ToggleGroupItem value="unlinked">
                {labels.state.unlinked}
              </ToggleGroupItem>
              <ToggleGroupItem value="undecided">
                {labels.state.undecided}
              </ToggleGroupItem>
            </ToggleGroup>
          </Field>
        );
      }}
    </MergeList>
  );
}

export function MergeAttachmentChoices({
  posts,
  value,
  disabled,
  onChange,
}: Props<"attachments">) {
  const msg = useMsg();
  const labels = useMergeLabels();
  const groups = new Map<string, PostMergePost["attachments"]>();
  for (const post of posts)
    for (const item of post.attachments)
      groups.set(attachmentKey(item), [
        ...(groups.get(attachmentKey(item)) ?? []),
        item,
      ]);
  return (
    <MergeList items={[...groups.entries()]} rowKey={([key]) => key}>
      {([, items]) => {
        const first = items[0];
        if (!first) return null;
        const { namespace, value: refValue } = first,
          key = attachmentKey(first);
        const selected = value.find((v) => attachmentKey(v) === key);
        const media = [
          ...new Map(
            items.flatMap((item) =>
              item.choice && item.media
                ? [[item.media.uuid, item.media] as const]
                : [],
            ),
          ).values(),
        ];
        const options = [
          {
            value: "preserve",
            label: msg("post_merge.preserve", "Preserve agreeing choices"),
          },
          { value: "unlinked", label: labels.state.unlinked },
          { value: "undecided", label: labels.state.undecided },
          ...media.map((item) => ({
            value: item.uuid,
            label: `${labels.state.linked} · ${labels.kind[item.kind]} ${item.local_id === null ? msg("post_merge.deleted", "Deleted item") : `#${item.local_id}`}`,
          })),
        ];
        return (
          <Choice
            label={`${namespace} · ${refValue}`}
            value={selected?.media_uuid ?? selected?.state ?? "preserve"}
            options={options}
            disabled={disabled}
            onChange={(next) => {
              const rest = value.filter((v) => attachmentKey(v) !== key);
              if (next === "preserve") onChange(rest);
              else if (next === "unlinked" || next === "undecided")
                onChange([
                  ...rest,
                  { namespace, value: refValue, state: next },
                ]);
              else
                onChange([
                  ...rest,
                  {
                    namespace,
                    value: refValue,
                    state: "linked",
                    media_uuid: next,
                  },
                ]);
            }}
          />
        );
      }}
    </MergeList>
  );
}
