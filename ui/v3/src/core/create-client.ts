import { getPlatformURL } from "./platform-url";
export { getPlatformURL } from "./platform-url";
import {
  ApolloClient,
  InMemoryCache,
  split,
  from,
  ServerError,
} from "@apollo/client";
import type { TypePolicies, FieldReadFunction } from "@apollo/client/cache";
import { GraphQLWsLink } from "@apollo/client/link/subscriptions";
import { createClient as createWSClient } from "graphql-ws";
import { onError } from "@apollo/client/link/error";
import { getMainDefinition } from "@apollo/client/utilities";
import UploadHttpLink from "apollo-upload-client/UploadHttpLink.mjs";

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
      paths: { merge: false },
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

export const createClient = () => {
  const url = getPlatformURL("graphql");

  const wsUrl = getPlatformURL("graphql");
  if (wsUrl.protocol === "https:") {
    wsUrl.protocol = "wss:";
  } else {
    wsUrl.protocol = "ws:";
  }

  const httpLink = new UploadHttpLink({ uri: url.toString() });

  const wsClient = createWSClient({
    url: wsUrl.toString(),
    retryAttempts: Infinity,
    shouldRetry() {
      return true;
    },
  });

  const wsLink = new GraphQLWsLink(wsClient);

  const errorLink = onError(({ error }) => {
    if (ServerError.is(error) && error.statusCode === 401) {
      if (import.meta.env.DEV) {
        alert(
          "GraphQL server error: 401 Unauthorized\n" +
            "Authentication cannot be used with the dev server, since the session authorization cookie cannot be sent cross-origin.\n" +
            "Please disable it on the server and refresh the page.",
        );
        return;
      }
      const newURL = new URL(
        getPlatformURL("login"),
        window.location.toString(),
      );
      newURL.searchParams.append("returnURL", window.location.href);
      window.location.href = newURL.toString();
    }
  });

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

  const link = from([errorLink, splitLink]);

  const cache = new InMemoryCache({
    typePolicies,
    possibleTypes,
  });

  const client = new ApolloClient({
    link,
    cache,
  });

  return { cache, client, wsClient };
};
