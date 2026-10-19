import { parseUIConfig } from "./config-schema";
import { getPlatformURL } from "./platform-url";
export { getPlatformURL } from "./platform-url";
import { ApolloClient, InMemoryCache, split, from } from "@apollo/client";
import type { TypePolicies, FieldReadFunction } from "@apollo/client/cache";
import { GraphQLWsLink } from "@apollo/client/link/subscriptions";
import { createClient as createWSClient } from "graphql-ws";
import { getMainDefinition } from "@apollo/client/utilities";
import UploadHttpLink from "apollo-upload-client/UploadHttpLink.mjs";
import { sessionFetch } from "./session-recovery";
import { websocketRecoveryOptions } from "./websocket-recovery";

const readReference = (typename: string): FieldReadFunction => {
  return (existing, { args, canRead, toReference }) =>
    canRead(existing)
      ? existing
      : toReference({
          __typename: typename,
          id: args?.id,
        });
};

const readDanglingNull: FieldReadFunction = (existing, { canRead }) => {
  if (existing === undefined) return undefined;
  return canRead(existing) ? existing : null;
};

const typePolicies: TypePolicies = {
  FindScenesResultType: { merge: true },
  FindImagesResultType: { merge: true },
  ConfigResult: {
    fields: {
      ui: { merge: (_existing, incoming: unknown) => parseUIConfig(incoming) },
    },
  },
  Query: {
    fields: {
      findImage: { read: readReference("Image") },
      findPerformer: { read: readReference("Performer") },
      findStudio: { read: readReference("Studio") },
      findGroup: { read: readReference("Group") },
      findGallery: { read: readReference("Gallery") },
      findScene: { read: readReference("Scene") },
      findTag: { read: readReference("Tag") },
      findSavedFilter: { read: readReference("SavedFilter") },
    },
  },
  Scene: {
    fields: {
      studio: { read: readDanglingNull },
      // Feed summaries select only screenshot. Retain the other paths from
      // scene details so refreshing a feed cannot invalidate active media.
      paths: { merge: true },
    },
  },
  Image: {
    fields: {
      studio: { read: readDanglingNull },
      paths: { merge: false },
    },
  },
  Group: {
    fields: { studio: { read: readDanglingNull } },
  },
  Gallery: {
    fields: {
      studio: { read: readDanglingNull },
      paths: { merge: false },
    },
  },
  Studio: {
    fields: { parent_studio: { read: readDanglingNull } },
  },
};

const possibleTypes = {
  BaseFile: ["VideoFile", "ImageFile", "GalleryFile"],
  VisualFile: ["VideoFile", "ImageFile"],
};

/** Keep entity normalization and partial-query merging consistent per client. */
export const createCache = () =>
  new InMemoryCache({ typePolicies, possibleTypes });

export const createClient = () => {
  const url = getPlatformURL("graphql");

  const wsUrl = getPlatformURL("graphql");
  if (wsUrl.protocol === "https:") {
    wsUrl.protocol = "wss:";
  } else {
    wsUrl.protocol = "ws:";
  }

  const httpLink = new UploadHttpLink({
    uri: url.toString(),
    credentials: "same-origin",
    fetch: sessionFetch,
  });

  const wsClient = createWSClient({
    url: wsUrl.toString(),
    ...websocketRecoveryOptions(),
  });

  const wsLink = new GraphQLWsLink(wsClient);

  const splitLink = split(
    ({ query }) => {
      const definition = getMainDefinition(query);
      return (
        definition.kind === "OperationDefinition" &&
        definition.operation === "subscription"
      );
    },
    wsLink,
    httpLink,
  );

  const link = from([splitLink]);

  const cache = createCache();

  const client = new ApolloClient({
    link,
    cache,
  });

  return { cache, client, wsClient };
};
