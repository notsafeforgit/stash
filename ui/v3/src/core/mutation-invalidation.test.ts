import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
  gql,
} from "@apollo/client";
import { describe, expect, it } from "vitest";
import {
  affectedActiveQueries,
  affectedQueryFields,
  rootFields,
} from "./mutation-invalidation";

describe("entity mutation invalidation", () => {
  it("includes changed relationships and aggregates, excluding unrelated services", () => {
    const fields = affectedQueryFields(
      gql`mutation { sceneUpdate(input: {}) { id } }`,
    );
    for (const field of [
      "findScenes",
      "findPerformer",
      "findStudio",
      "findGroups",
      "findTags",
      "stats",
    ])
      expect(fields.has(field)).toBe(true);
    for (const field of [
      "configuration",
      "plugins",
      "systemStatus",
      "jobQueue",
      "findImages",
    ])
      expect(fields.has(field)).toBe(false);
  });

  it("handles aliases and root fragments", () => {
    expect(
      rootFields(
        gql`query { ...Page } fragment Page on Query { items: findScenes { count } ... on Query { stats { scene_count } } }`,
      ),
    ).toEqual(["findScenes", "stats"]);
  });

  it("refetches only active affected queries in the shared cache", () => {
    const client = new ApolloClient({
      cache: new InMemoryCache(),
      link: new ApolloLink(() => new Observable(() => {})),
    });
    const scenes = gql`query Scenes { findScenes { count } }`;
    const config = gql`query Config { configuration { ui } }`;
    const inactive = gql`query Tags { findTags { count } }`;
    const subscriptions = [scenes, config].map((query) =>
      client.watchQuery({ query }).subscribe({}),
    );
    client.watchQuery({ query: inactive });
    expect(
      affectedActiveQueries(
        client,
        gql`mutation { sceneDestroy(input: {}) }`,
      ).map(rootFields),
    ).toEqual([["findScenes"]]);
    for (const subscription of subscriptions) subscription.unsubscribe();
    client.stop();
  });
});
