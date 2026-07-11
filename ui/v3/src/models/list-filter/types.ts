import type { CriterionValue, SavedCriterion } from "./criteria/criterion";

export type SavedObjectFilter = {
  [K in CriterionType]?: SavedCriterion<CriterionValue>;
};

// Persisted shape of SavedFilter.filter_ast: the canonical criteria tree.
// `field` is the client criterion type; `value` is the labeled
// saved-criterion shape ({modifier?, value?, field?}). Mirrors
// FilterASTInput in the server schema.
export interface SavedASTConditionNode {
  condition: { field: string; value: Record<string, unknown> };
}
export interface SavedASTGroupNode {
  group: { operator: string; children: SavedASTNode[] };
}
export type SavedASTNode = SavedASTConditionNode | SavedASTGroupNode;
export type SavedFilterAST = { root: SavedASTNode };

// Saved filters intentionally carry no UI preferences. Display mode and
// zoom are per-view localStorage prefs (see `useDisplayModePref` /
// `useZoomPref`); a saved filter is a snapshot of the data the user
// asked the server for, not how the client chose to render it.
export type SavedUIOptions = Record<string, never>;

export enum DisplayMode {
  Grid,
  Wall,
  Tagger,
  Details,
  Table,
}

export interface ILabeledId {
  id: string;
  label: string;
}

export interface ILabeledValue {
  label: string;
  value: string;
}

export interface ILabeledValueListValue {
  items: ILabeledId[];
  excluded: ILabeledId[];
}

export interface IHierarchicalLabelValue {
  items: ILabeledId[];
  excluded: ILabeledId[];
  depth: number;
  hierarchyMode?:
    | "exact"
    | "ancestors"
    | "descendants"
    | "ancestors_descendants";
}

export interface IRangeValue<V> {
  value: V | undefined;
  value2: V | undefined;
}

export type INumberValue = IRangeValue<number>;
export interface IHierarchicalCountValue extends INumberValue {
  depth: number;
}
export type IDateValue = IRangeValue<string>;
export type ITimestampValue = IRangeValue<string>;
export interface IDuplicationValue {
  // Deprecated: Use phash field instead. Kept for backwards compatibility.
  duplicated?: boolean;
  // Currently not implemented. Intended for phash distance matching.
  distance?: number;
  phash?: boolean;
  url?: boolean;
  stash_id?: boolean;
  title?: boolean;
}

export interface IStashIDValue {
  endpoint: string;
  stashID: string;
}

export interface IPhashDistanceValue {
  value: string;
  distance?: number;
}

export function criterionIsHierarchicalLabelValue(
  value: unknown,
): value is IHierarchicalLabelValue {
  return (
    typeof value === "object" && !!value && "items" in value && "depth" in value
  );
}

export function criterionIsNumberValue(value: unknown): value is INumberValue {
  return (
    typeof value === "object" &&
    !!value &&
    "value" in value &&
    "value2" in value
  );
}

export function criterionIsStashIDValue(
  value: unknown,
): value is IStashIDValue {
  return (
    typeof value === "object" &&
    !!value &&
    "endpoint" in value &&
    "stashID" in value
  );
}

export function criterionIsDateValue(value: unknown): value is IDateValue {
  return (
    typeof value === "object" &&
    !!value &&
    "value" in value &&
    "value2" in value
  );
}

export function criterionIsTimestampValue(
  value: unknown,
): value is ITimestampValue {
  return (
    typeof value === "object" &&
    !!value &&
    "value" in value &&
    "value2" in value
  );
}

export interface IOptionType {
  id: string;
  name?: string;
  image_path?: string;
}

export type CriterionType =
  | "names"
  | "path"
  | "rating100"
  | "organized"
  | "o_counter"
  | "resolution"
  | "average_resolution"
  | "framerate"
  | "bitrate"
  | "bit_depth"
  | "video_stream_duration"
  | "frame_count"
  | "duration_mismatch"
  | "video_codec"
  | "audio_codec"
  | "duration"
  | "filter_favorites"
  | "favorite"
  | "has_markers"
  | "is_missing"
  | "tags"
  | "scene_tags"
  | "performer_tags"
  | "studio_tags"
  | "tag_count"
  | "performers"
  | "studios"
  | "scenes"
  | "groups"
  | "movies" // legacy
  | "containing_groups"
  | "containing_group_count"
  | "sub_groups"
  | "sub_group_count"
  | "galleries"
  | "birth_year"
  | "age"
  | "ethnicity"
  | "country"
  | "hair_color"
  | "eye_color"
  | "height_cm"
  | "weight"
  | "measurements"
  | "fake_tits"
  | "penis_length"
  | "circumcised"
  | "career_length"
  | "career_start"
  | "career_end"
  | "tattoos"
  | "piercings"
  | "aliases"
  | "gender"
  | "parents"
  | "children"
  | "scene_count"
  | "marker_count"
  | "image_count"
  | "gallery_count"
  | "performer_count"
  | "studio_count"
  | "group_count"
  | "death_year"
  | "url"
  | "interactive"
  | "interactive_speed"
  | "captions"
  | "resume_time"
  | "play_count"
  | "play_duration"
  | "last_played_at"
  | "name"
  | "details"
  | "title"
  | "oshash"
  | "orientation"
  | "checksum"
  | "phash_distance"
  | "director"
  | "synopsis"
  | "parent_count"
  | "child_count"
  | "performer_favorite"
  | "favorite"
  | "performer_age"
  | "duplicated"
  | "ignore_auto_tag"
  | "file_count"
  | "stash_id_endpoint"
  | "stash_id_count"
  | "date"
  | "created_at"
  | "updated_at"
  | "birthdate"
  | "death_date"
  | "scene_date"
  | "scene_created_at"
  | "scene_updated_at"
  | "description"
  | "code"
  | "photographer"
  | "disambiguation"
  | "has_chapters"
  | "sort_name"
  | "custom_fields"
  | "custom_field"
  | "folder"
  | "parent_folder";
