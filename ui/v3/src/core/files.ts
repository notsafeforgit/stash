import { fileStemFromPath } from "src/utils/file";
import type * as GQL from "src/core/generated-graphql";

export interface File {
  path: string;
}

interface ObjectWithFiles {
  files?: GQL.Maybe<File[]>;
}

export interface ObjectWithTitleFiles extends ObjectWithFiles {
  title?: GQL.Maybe<string>;
}

export function objectTitle(s: Partial<ObjectWithTitleFiles>) {
  if (s.title) {
    return s.title;
  }
  if (s.files && s.files.length > 0) {
    return fileStemFromPath(s.files[0].path);
  }
  return "";
}

export function objectPath(s: ObjectWithFiles) {
  if (s.files && s.files.length > 0) {
    return s.files[0].path;
  }
  return "";
}

interface ObjectWithVisualFiles {
  visual_files?: File[];
}

export interface ObjectWithTitleVisualFiles extends ObjectWithVisualFiles {
  title?: GQL.Maybe<string>;
}

export function imageTitle(s: Partial<ObjectWithTitleVisualFiles>) {
  if (s.title) {
    return s.title;
  }
  if (s.visual_files && s.visual_files.length > 0) {
    return fileStemFromPath(s.visual_files[0].path);
  }
  return "";
}

export function imagePath(s: ObjectWithVisualFiles) {
  if (s.visual_files && s.visual_files.length > 0) {
    return s.visual_files[0].path;
  }
  return "";
}
