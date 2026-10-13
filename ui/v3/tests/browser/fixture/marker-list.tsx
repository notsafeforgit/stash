import { useMemo } from "react";
import { ApolloClient, ApolloLink, Observable } from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { createCache } from "@/core/create-client";
import { Route as markersRoute } from "@/routes/scenes/markers";
import { scenes } from "./scene-lightbox";
import type { FilterAstNodeInput } from "@/core/generated-graphql";
import { Toaster } from "@/components/ui/sonner";

declare global {
  interface Window {
    markerListMutations: { operation: string; id?: string; ids?: string[] }[];
    markerListBulk?: boolean;
    markerListFailBatch?: number;
    markerListIncomplete?: boolean;
    markerListQueries: { operation: string; variables: unknown }[];
    markerListRemaining: string[];
  }
}

function makeClient() {
  const scene = scenes[0];
  if (!scene) throw new Error("Missing scene fixture");
  const source = window.markerListBulk
    ? Array.from({ length: 65 }, (_, index) => ({
        ...scene.scene_markers[index % scene.scene_markers.length]!,
        title:
          index < 63
            ? `Selected clip ${index + 1}`
            : index === 63
              ? "Selected photo"
              : "Other clip",
      }))
    : scene.scene_markers;
  let markers = source.map((marker, index) => ({
    ...marker,
    id: `00000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
    screenshot: scene.paths.screenshot ?? "",
    end_seconds: window.markerListBulk
      ? marker.seconds + (index < 64 ? 5 : 0)
      : marker.end_seconds,
  }));
  window.markerListMutations = [];
  window.markerListQueries = [];
  window.markerListRemaining = markers.map((marker) => marker.id);
  let batchCount = 0;

  function matches(
    marker: (typeof markers)[number],
    node?: FilterAstNodeInput,
  ): boolean {
    if (!node) return true;
    if (node.group) {
      const children = node.group.children.map((child) =>
        matches(marker, child),
      );
      return node.group.operator === "AND"
        ? children.every(Boolean)
        : children.some(Boolean);
    }
    const value = node.condition?.value;
    if (
      node.condition?.field === "duration" &&
      value &&
      typeof value === "object" &&
      "value" in value &&
      typeof value.value === "number"
    )
      return (
        (marker.end_seconds ?? marker.seconds) - marker.seconds > value.value
      );
    throw new Error("Unexpected marker fixture condition");
  }
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
            case "FindSceneMarkerIDs": {
              window.markerListQueries.push({
                operation: operationName,
                variables,
              });
              const matching = markers.filter(
                (marker) =>
                  matches(marker, variables.scene_marker_filter_ast?.root) &&
                  marker.title
                    .toLowerCase()
                    .includes((variables.filter?.q ?? "").toLowerCase()),
              );
              const perPage = variables.filter?.per_page ?? 40;
              const offset = ((variables.filter?.page ?? 1) - 1) * perPage;
              const page =
                perPage === -1
                  ? matching
                  : matching.slice(offset, offset + perPage);
              data = {
                findSceneMarkers: {
                  __typename: "FindSceneMarkersResultType",
                  count: matching.length,
                  scene_markers:
                    operationName === "FindSceneMarkerIDs" &&
                    window.markerListIncomplete
                      ? page.slice(0, 1)
                      : page,
                },
              };
              break;
            }
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
            case "SceneMarkersDestroy":
              window.markerListMutations.push({
                operation: operationName,
                ids: variables.ids,
              });
              batchCount++;
              if (batchCount === window.markerListFailBatch) {
                data = { sceneMarkersDestroy: false };
                break;
              }
              markers = markers.filter(
                (marker) => !variables.ids.includes(marker.id),
              );
              window.markerListRemaining = markers.map((marker) => marker.id);
              data = { sceneMarkersDestroy: true };
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
      <Toaster />
    </ApolloProvider>
  );
}
