import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
  gql,
} from "@apollo/client";
import { afterEach, expect, it, vi } from "vitest";
import { invalidateAfterEntityJob } from "./entity-job-invalidation";

afterEach(() => vi.useRealTimers());

it.each([
  "FINISHED",
  "FAILED",
  "CANCELLED",
])("refreshes after a bulk job ends with %s, even when the sheet is gone", async (status) => {
  vi.useFakeTimers();
  let finished = false;
  let sceneRequests = 0;
  let jobRequests = 0;
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          if (operation.operationName === "FindJob") {
            jobRequests++;
            observer.next({
              data: {
                findJob: {
                  __typename: "Job",
                  id: "1",
                  status: finished ? status : "RUNNING",
                  description: "Bulk update",
                  progress: 0,
                  subTasks: [],
                  error: null,
                  addTime: "2026-09-05T00:00:00Z",
                  startTime: null,
                  endTime: null,
                },
              },
            });
          } else {
            sceneRequests++;
            observer.next({
              data: {
                findScenes: {
                  __typename: "FindScenesResultType",
                  count: finished ? 2 : 1,
                },
              },
            });
          }
          observer.complete();
        }),
    ),
  });
  const scenes = client
    .watchQuery({ query: gql`query Scenes { findScenes { count } }` })
    .subscribe({});
  const mutation = gql`mutation { bulkSceneUpdateJob(input: {}) }`;
  invalidateAfterEntityJob(client, mutation, "1");
  invalidateAfterEntityJob(client, mutation, "1");
  await vi.advanceTimersByTimeAsync(0);
  expect(sceneRequests).toBe(1);
  expect(jobRequests).toBe(1);
  finished = true;
  await vi.advanceTimersByTimeAsync(1100);
  expect(sceneRequests).toBe(2);
  const completedRequests = jobRequests;
  await vi.advanceTimersByTimeAsync(2000);
  expect(jobRequests).toBe(completedRequests);
  scenes.unsubscribe();
  client.stop();
});
