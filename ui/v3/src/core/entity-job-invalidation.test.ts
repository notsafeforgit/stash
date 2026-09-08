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

it("distinguishes synchronous acknowledgments and numeric jobs", async () => {
  const { decodeEntityJobAcknowledgment } = await import(
    "./entity-job-invalidation"
  );
  expect(decodeEntityJobAcknowledgment("sync")).toEqual({ kind: "completed" });
  expect(decodeEntityJobAcknowledgment("12")).toEqual({
    kind: "scheduled",
    id: "12",
  });
  for (const value of [null, "", "abc", "1.5", "-1", "9007199254740993"])
    expect(decodeEntityJobAcknowledgment(value)).toEqual({ kind: "invalid" });
});

it.each([
  1, 10,
])("retries %s query failures and disposes terminal monitors", async (failures) => {
  vi.useFakeTimers();
  const errors = vi.spyOn(console, "error").mockImplementation(() => {});
  let requests = 0;
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      () =>
        new Observable((observer) => {
          requests++;
          if (requests <= failures) observer.error(new Error("Unavailable"));
          else {
            observer.next({ data: { findJob: null } });
            observer.complete();
          }
        }),
    ),
  });
  const refetch = vi.spyOn(client, "refetchQueries");
  const mutation = gql`mutation { bulkSceneUpdateJob(input: {}) }`;
  invalidateAfterEntityJob(client, mutation, "sync");
  await vi.advanceTimersByTimeAsync(0);
  expect(requests).toBe(0);
  const stop = invalidateAfterEntityJob(client, mutation, "7");
  await vi.advanceTimersByTimeAsync(7000);
  expect(requests).toBe(failures === 1 ? 2 : 3);
  expect(refetch).toHaveBeenCalledOnce();
  const completed = requests;
  await vi.advanceTimersByTimeAsync(10000);
  expect(requests).toBe(completed);
  // A failed ID is released, so a later operation may monitor it again.
  const stopAgain = invalidateAfterEntityJob(client, mutation, "7");
  await vi.advanceTimersByTimeAsync(0);
  expect(requests).toBe(completed + 1);
  stopAgain();
  stop();
  client.stop();
  errors.mockRestore();
});

it("explicit disposal cancels a pending job retry", async () => {
  vi.useFakeTimers();
  let requests = 0;
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      () =>
        new Observable((observer) => {
          requests++;
          observer.error(new Error("Offline"));
        }),
    ),
  });
  const dispose = invalidateAfterEntityJob(
    client,
    gql`mutation { bulkSceneUpdateJob(input: {}) }`,
    "9",
  );
  await vi.advanceTimersByTimeAsync(0);
  dispose();
  await vi.advanceTimersByTimeAsync(10000);
  expect(requests).toBe(1);
  client.stop();
});
