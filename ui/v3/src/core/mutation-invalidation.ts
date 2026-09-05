import type { ApolloClient, DocumentNode } from "@apollo/client";
import type { SelectionSetNode } from "graphql";

const queryFields = {
  scene: [
    "findScene",
    "findSceneByHash",
    "findScenes",
    "findScenesByPathRegex",
    "findDuplicateScenes",
    "findDuplicateSceneGroups",
    "sceneWall",
    "sceneStreams",
    "parseSceneFilenames",
  ],
  marker: [
    "findSceneMarkers",
    "markerWall",
    "markerStrings",
    "sceneMarkerTags",
  ],
  image: [
    "findImage",
    "findImages",
    "findDuplicateImages",
    "findDuplicateImageGroups",
  ],
  gallery: ["findGallery", "findGalleries"],
  performer: ["findPerformer", "findPerformers"],
  studio: ["findStudio", "findStudios"],
  group: ["findGroup", "findGroups", "findMovie", "findMovies"],
  tag: ["findTag", "findTags"],
  file: ["findFile", "findFiles", "findFolder", "findFolders"],
} as const;
type Entity = keyof typeof queryFields;
const allEntities = Object.keys(queryFields) as Entity[];

// Include parents whose counts or relationship lists can change. Keeping this
// policy at the domain boundary makes new mutation callers consistent.
const affectedEntities: Record<Entity, readonly Entity[]> = {
  scene: [
    "scene",
    "marker",
    "gallery",
    "performer",
    "studio",
    "group",
    "tag",
    "file",
  ],
  image: ["image", "gallery", "performer", "studio", "tag", "file"],
  gallery: ["gallery", "image", "scene", "performer", "studio", "tag", "file"],
  performer: allEntities,
  studio: allEntities,
  tag: allEntities,
  group: ["group", "scene", "performer", "studio", "tag", "marker"],
  marker: ["marker", "scene", "tag"],
  file: allEntities,
};

export function rootFields(document: DocumentNode): string[] {
  const fields = new Set<string>();
  const visited = new Set<string>();
  function collect(set: SelectionSetNode) {
    for (const selection of set.selections) {
      if (selection.kind === "Field") fields.add(selection.name.value);
      else if (selection.kind === "InlineFragment")
        collect(selection.selectionSet);
      else if (!visited.has(selection.name.value)) {
        visited.add(selection.name.value);
        const fragment = document.definitions.find(
          (d) =>
            d.kind === "FragmentDefinition" &&
            d.name.value === selection.name.value,
        );
        if (fragment?.kind === "FragmentDefinition")
          collect(fragment.selectionSet);
      }
    }
  }
  for (const definition of document.definitions) {
    if (definition.kind === "OperationDefinition")
      collect(definition.selectionSet);
  }
  return [...fields];
}

export function affectedQueryFields(mutation: DocumentNode): Set<string> {
  const entities = new Set<Entity>();
  for (const field of rootFields(mutation)) {
    const name = field.replace(/^bulk/, "").toLowerCase();
    const entity = name.startsWith("scenemarker")
      ? "marker"
      : allEntities.find((candidate) => name.startsWith(candidate));
    // An unclassified library operation gets the conservative library scope,
    // never unrelated configuration, plugins, or system-status queries.
    for (const affected of entity ? affectedEntities[entity] : allEntities)
      entities.add(affected);
  }
  return new Set([
    "stats",
    "customFieldNames",
    ...[...entities].flatMap((e) => queryFields[e]),
  ]);
}

export function affectedActiveQueries(
  client: ApolloClient,
  mutation: DocumentNode,
): DocumentNode[] {
  const fields = affectedQueryFields(mutation);
  return [...client.getObservableQueries("active")]
    .filter((query) =>
      rootFields(query.query).some((field) => fields.has(field)),
    )
    .map((query) => query.query);
}
