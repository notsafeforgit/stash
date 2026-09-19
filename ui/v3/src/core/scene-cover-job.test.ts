import { ApolloClient, ApolloLink, Observable, gql } from "@apollo/client";
import type { TypedDocumentNode } from "@graphql-typed-document-node/core";
import { afterEach, expect, it, vi } from "vitest";
import { createCache } from "./create-client";
import {
  JobStatus,
  PreviewImageDynamicRange,
  type FindSceneCoversQuery,
  type PreviewImageDataFragment,
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
};
const detailQuery: TypedDocumentNode<{
  findScene: Omit<typeof detail, "preview_image"> & {
    preview_image: PreviewImageDataFragment | null;
  };
}> = gql`
  query CoverTestScene {
    findScene(id: "1") {
      id title resume_time files { id updated_at }
      sceneStreams: sceneStreamsV3 { url mime_type label }
      paths { screenshot stream sprite }
      preview_image { fallback sources { url mime_type dynamic_range width height } }
    }
  }
`;
const listQuery: TypedDocumentNode<FindSceneCoversQuery> = gql`
  query CoverTestList {
    findScenes {
      scenes {
        id paths { screenshot }
        preview_image { fallback sources { url mime_type dynamic_range width height } }
      }
    }
  }
`;
const covers: FindSceneCoversQuery = {
  findScenes: {
    __typename: "FindScenesResultType",
    scenes: [
      {
        __typename: "Scene",
        id: "1",
        paths: { __typename: "ScenePathsType", screenshot: "/new.jpg" },
        preview_image: {
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

it.each([JobStatus.Finished, JobStatus.Failed, JobStatus.Cancelled, null])(
  "publishes only artwork after %s, even after the initiating view leaves",
  async (status) => {
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
    const task = refreshSceneCoversAfterJob(client, ["1", "1"], "7");
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
    expect(requests).toEqual(["FindJob", "FindJob", "FindSceneCovers"]);
    await vi.advanceTimersByTimeAsync(5000);
    expect(requests).toHaveLength(3);
    list.unsubscribe();
    client.stop();
  },
);
