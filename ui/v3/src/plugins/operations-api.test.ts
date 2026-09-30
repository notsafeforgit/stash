import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { expect, it } from "vitest";
import { createPluginOperationsAPI } from "./operations-api";

it("binds operations to the owning plugin, keeps queries fresh, and propagates errors", async () => {
  const requests: {
    name: string | undefined;
    variables: Record<string, unknown>;
  }[] = [];
  const apollo = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          requests.push({
            name: operation.operationName,
            variables: operation.variables,
          });
          if (operation.variables.operation === "fail") {
            observer.error(new Error("Review is stale"));
          } else {
            observer.next({
              data: {
                [operation.operationName === "PluginQueryV3"
                  ? "pluginQueryV3"
                  : "pluginMutationV3"]: { revision: requests.length },
              },
            });
            observer.complete();
          }
        }),
    ),
  });
  try {
    const api = createPluginOperationsAPI(apollo, "catalog");
    expect(await api.query("review")).toEqual({ revision: 1 });
    expect(await api.query("review")).toEqual({ revision: 2 });
    expect(
      await api.mutate("apply", {
        plugin_id: "other",
        review_token: "reviewed",
      }),
    ).toEqual({ revision: 3 });
    expect(requests[2]).toEqual({
      name: "PluginMutationV3",
      variables: {
        plugin_id: "catalog",
        operation: "apply",
        input: { plugin_id: "other", review_token: "reviewed" },
      },
    });
    await expect(api.mutate("fail")).rejects.toThrow("Review is stale");
  } finally {
    apollo.stop();
  }
});
