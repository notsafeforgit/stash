import { ApolloClient, ApolloLink, Observable, gql } from "@apollo/client";
import type { TypedDocumentNode } from "@graphql-typed-document-node/core";
import { print } from "graphql";
import { afterEach, expect, it, vi } from "vitest";
import { createCache } from "./create-client";
import {
  JobStatus,
  PreviewImageDynamicRange,
  PreviewImageDataFragmentDoc,
  SceneCoverOriginDataFragmentDoc,
  SceneCoverOriginStatus,
  type SceneCoverOriginDataFragment,
  type FindSceneCoversQuery,
  type PreviewImageDataFragment,
  FindSceneListCountDocument,
  FilterGroupOperator,
  type FindSceneListCountQueryVariables,
} from "./generated-graphql";
import { refreshSceneCoversAfterJob } from "./scene-cover-job";

afterEach(() => vi.useRealTimers());

const detail = {
  __typename: "Scene" as const,
  id: "1",
  title: "Original title",
  resume_time: 123,
  files: [{ __typename: "VideoFile" as const, id: "7", updated_at: "old" }],
  sceneStreams: [
    {
      url: "/stream?signature=original",
      mime_type: "video/mp4",
      label: "Direct",
    },
  ],
  paths: {
    __typename: "ScenePathsType" as const,
    screenshot: "/old.jpg",
    stream: "/stream",
    sprite: "/sprite.jpg",
  },
  preview_image: null,
  cover_origin: null,
};
const detailQuery: TypedDocumentNode<{
  findScene: Omit<typeof detail, "preview_image" | "cover_origin"> & {
    preview_image: PreviewImageDataFragment | null;
    cover_origin: SceneCoverOriginDataFragment | null;
  };
}> = gql`
  query CoverTestScene {
    findScene(id: "1") {
      id title resume_time files { id updated_at }
      sceneStreams: sceneStreamsV3 { url mime_type label }
      paths { screenshot stream sprite }
      preview_image { ...PreviewImageData }
      cover_origin { ...SceneCoverOriginData }
    }
  }
  ${print(PreviewImageDataFragmentDoc)}
  ${print(SceneCoverOriginDataFragmentDoc)}
`;
const listQuery: TypedDocumentNode<FindSceneCoversQuery> = gql`
  query CoverTestList {
    findScenes {
      scenes {
        id paths { screenshot }
        preview_image { ...PreviewImageData }
        cover_origin { ...SceneCoverOriginData }
      }
    }
  }
  ${print(PreviewImageDataFragmentDoc)}
  ${print(SceneCoverOriginDataFragmentDoc)}
`;
const covers: FindSceneCoversQuery = {
  findScenes: {
    __typename: "FindScenesResultType",
    scenes: [
      {
        __typename: "Scene",
        id: "1",
        cover_origin: {
          __typename: "SceneCoverOrigin",
          at: 0,
          source_file_id: "7",
          status: SceneCoverOriginStatus.Available,
        },
        paths: { __typename: "ScenePathsType", screenshot: "/new.jpg" },
        preview_image: {
          thumbnail: {
            __typename: "PreviewImage",
            fallback: "/thumbnail.jpg",
            sources: [
              {
                __typename: "PreviewImageSource",
                url: "/thumbnail.avif",
                mime_type: "image/avif",
                dynamic_range: PreviewImageDynamicRange.Adaptive,
                width: 80,
                height: 45,
              },
            ],
          },
          __typename: "PreviewImage",
          fallback: "/new.jpg",
          sources: [
            {
              __typename: "PreviewImageSource",
              url: "/new.avif",
              mime_type: "image/avif",
              dynamic_range: PreviewImageDynamicRange.Hdr,
              width: 160,
              height: 90,
            },
          ],
        },
      },
    ],
  },
};

it.each(
  [JobStatus.Finished, JobStatus.Failed, JobStatus.Cancelled, null].flatMap(
    (status) => [
      { status, scope: "selected" },
      { status, scope: "cached" },
    ],
  ),
)(
  "publishes only $scope artwork after $status, even after the initiating view leaves",
  async ({ status, scope }) => {
    vi.useFakeTimers();
    let finished = false;
    const requests: string[] = [];
    const client = new ApolloClient({
      cache: createCache(),
      link: new ApolloLink(
        (operation) =>
          new Observable((observer) => {
            requests.push(operation.operationName ?? "");
            if (operation.operationName === "FindJob") {
              observer.next({
                data: {
                  findJob:
                    finished && status === null
                      ? null
                      : {
                          __typename: "Job",
                          id: "7",
                          status: finished ? status : JobStatus.Running,
                          description: "Generating",
                          addTime: "2026-09-16T00:00:00Z",
                          subTasks: [],
                          progress: 0,
                          error: null,
                          startTime: null,
                          endTime: null,
                        },
                },
              });
            } else if (operation.operationName === "FindSceneCovers") {
              expect(operation.variables).toEqual({ ids: ["1"] });
              observer.next({ data: covers });
            } else
              observer.error(
                new Error(`Unexpected query ${operation.operationName}`),
              );
            observer.complete();
          }),
      ),
    });
    // These scenes enter the cache after the job starts, as when returning
    // from Settings while a library-wide reset is running.
    const task = refreshSceneCoversAfterJob(
      client,
      scope === "cached" ? "cached" : ["1", "1"],
      "7",
    );
    client.writeQuery({ query: detailQuery, data: { findScene: detail } });
    client.writeQuery({
      query: listQuery,
      data: {
        findScenes: {
          __typename: "FindScenesResultType",
          scenes: [detail],
        },
      },
    });
    const page = client.watchQuery({ query: detailQuery }).subscribe({});
    const before = client.readQuery({ query: detailQuery })?.findScene;
    page.unsubscribe();
    let visible: FindSceneCoversQuery | undefined;
    const list = client.watchQuery({ query: listQuery }).subscribe((result) => {
      if (result.dataState === "complete") visible = result.data;
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(requests).toEqual(["FindJob"]);
    finished = true;
    await vi.advanceTimersByTimeAsync(1100);
    expect((await task).kind).toBe("complete");
    expect(visible).toEqual(covers);
    const after = client.readQuery({ query: detailQuery })?.findScene;
    expect(after?.sceneStreams).toBe(before?.sceneStreams);
    expect(after?.files).toBe(before?.files);
    expect(after?.title).toBe(detail.title);
    expect(after?.resume_time).toBe(detail.resume_time);
    expect(after?.paths).toEqual({ ...detail.paths, screenshot: "/new.jpg" });
    expect(after?.cover_origin).toEqual(
      covers.findScenes.scenes[0]?.cover_origin,
    );
    expect(requests).toEqual(["FindJob", "FindJob", "FindSceneCovers"]);
    await vi.advanceTimersByTimeAsync(5000);
    expect(requests).toHaveLength(3);
    list.unsubscribe();
    client.stop();
  },
);

it("refreshes active cover-filtered counts after a job without refetching playback or unrelated lists", async () => {
  const variables: FindSceneListCountQueryVariables = {
    scene_filter_ast: {
      root: {
        group: {
          operator: FilterGroupOperator.Or,
          children: [
            {
              condition: {
                field: "cover_frame",
                value: { modifier: "EQUALS", value: "SPECIFIC" },
              },
            },
          ],
        },
      },
    },
  };
  const requests: string[] = [];
  const client = new ApolloClient({
    cache: createCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          requests.push(operation.operationName ?? "");
          if (operation.operationName === "FindJob")
            observer.next({
              data: {
                findJob: {
                  __typename: "Job",
                  id: "7",
                  status: JobStatus.Finished,
                  description: "Generating",
                  addTime: "2026-09-16T00:00:00Z",
                  subTasks: [],
                  progress: 1,
                  error: null,
                  startTime: null,
                  endTime: null,
                },
              },
            });
          else if (operation.operationName === "FindSceneListCount") {
            expect(operation.variables).toEqual(variables);
            observer.next({
              data: {
                result: { __typename: "FindScenesResultType", count: 0 },
              },
            });
          } else
            observer.error(
              new Error(`Unexpected query ${operation.operationName}`),
            );
          observer.complete();
        }),
    ),
  });
  for (const scope of [variables, {}])
    client.writeQuery({
      query: FindSceneListCountDocument,
      variables: scope,
      data: { result: { __typename: "FindScenesResultType", count: 2 } },
    });
  client.writeQuery({ query: detailQuery, data: { findScene: detail } });
  const subscriptions = [
    client.watchQuery({ query: detailQuery }).subscribe({}),
    client
      .watchQuery({ query: FindSceneListCountDocument, variables })
      .subscribe({}),
    client
      .watchQuery({ query: FindSceneListCountDocument, variables: {} })
      .subscribe({}),
  ];
  try {
    await refreshSceneCoversAfterJob(client, [], "7");
    expect(
      client.readQuery({ query: FindSceneListCountDocument, variables })?.result
        .count,
    ).toBe(0);
    expect(
      client.readQuery({ query: FindSceneListCountDocument, variables: {} })
        ?.result.count,
    ).toBe(2);
    expect(
      client.readQuery({ query: detailQuery })?.findScene.sceneStreams,
    ).toEqual(detail.sceneStreams);
    expect(requests).toEqual(["FindJob", "FindSceneListCount"]);
  } finally {
    for (const subscription of subscriptions) subscription.unsubscribe();
    client.stop();
  }
});
