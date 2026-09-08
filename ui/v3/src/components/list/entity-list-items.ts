import type * as GQL from "@/core/generated-graphql";
import { createListItemsContext } from "./list-items-context";

export const SceneListItems =
  createListItemsContext<GQL.SlimSceneDataFragment>();
export const ImageListItems =
  createListItemsContext<GQL.FindImagesQuery["findImages"]["images"][number]>();
export const GalleryListItems =
  createListItemsContext<
    GQL.FindGalleriesQuery["findGalleries"]["galleries"][number]
  >();
export const PerformerListItems =
  createListItemsContext<
    GQL.FindPerformersQuery["findPerformers"]["performers"][number]
  >();
export const GroupListItems =
  createListItemsContext<GQL.FindGroupsQuery["findGroups"]["groups"][number]>();
export const StudioListItems =
  createListItemsContext<
    GQL.FindStudiosQuery["findStudios"]["studios"][number]
  >();
export const TagListItems =
  createListItemsContext<GQL.FindTagsQuery["findTags"]["tags"][number]>();
