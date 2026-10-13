import { useMemo } from "react";
import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { createCache } from "@/core/create-client";
import { Route as markersRoute } from "@/routes/scenes/markers";
import { scenes } from "./scene-lightbox";

declare global {
  interface Window {
    markerListMutations: { operation: string; id?: string }[];
  }
}

function makeClient() {
  const scene = scenes[0];
  if (!scene) throw new Error("Missing scene fixture");
  let markers = scene.scene_markers.map((marker, index) => ({
    ...marker,
    id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
    screenshot: scene.paths.screenshot ?? "",
  }));
  window.markerListMutations = [];
  return new ApolloClient({
    cache: createCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          const { operationName, variables } = operation;
          let data: object;
          switch (operationName) {
            case "FindSavedFilters":
              data = { findSavedFilters: [] };
              break;
            case "FindSceneMarkers":
              data = {
                findSceneMarkers: {
                  __typename: "FindSceneMarkersResultType",
                  count: markers.length,
                  scene_markers: markers,
                },
              };
              break;
            case "FindScene":
              data = { findScene: { ...scene, scene_markers: markers } };
              break;
            case "SceneMarkerDestroy":
              window.markerListMutations.push({
                operation: operationName,
                id: variables.id,
              });
              markers = markers.filter((marker) => marker.id !== variables.id);
              data = { sceneMarkerDestroy: true };
              break;
            case "SceneSaveActivity":
              data = { sceneSaveActivity: true };
              break;
            default:
              observer.error(
                new Error(`Unexpected marker operation ${operationName}`),
              );
              return;
          }
          observer.next({ data });
          observer.complete();
        }),
    ),
  });
}

export function MarkerListFixture() {
  const client = useMemo(makeClient, []);
  const MarkersPage = markersRoute.options.component;
  if (!MarkersPage) throw new Error("Missing marker route component");
  return (
    <ApolloProvider client={client}>
      <MarkersPage />
    </ApolloProvider>
  );
}
