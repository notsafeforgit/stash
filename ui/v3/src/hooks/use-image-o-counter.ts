/**
 * Image O-counter mutations with optimistic cache updates.
 *
 * Each call site (toolbar, lightbox footer) wires the same three
 * mutations against the same Image cache entry; this hook collapses
 * the boilerplate into one place.
 */
import { useMutation } from "@apollo/client/react";
import type { ApolloCache } from "@apollo/client";
import * as GQL from "src/core/generated-graphql";

function modifyCounter(cache: ApolloCache, imageId: string, next: number) {
  cache.modify({
    id: cache.identify({ __typename: "Image", id: imageId }),
    fields: { o_counter: () => next },
  });
}

export function useImageOCounter(imageId: string) {
  const [incrementO] = useMutation(GQL.ImageIncrementODocument, {
    variables: { id: imageId },
    update(cache, { data }) {
      if (data?.imageIncrementO == null) return;
      modifyCounter(cache, imageId, data.imageIncrementO);
    },
  });
  const [decrementO] = useMutation(GQL.ImageDecrementODocument, {
    variables: { id: imageId },
    update(cache, { data }) {
      if (data?.imageDecrementO == null) return;
      modifyCounter(cache, imageId, data.imageDecrementO);
    },
  });
  const [resetO] = useMutation(GQL.ImageResetODocument, {
    variables: { id: imageId },
    update(cache, { data }) {
      if (data?.imageResetO == null) return;
      modifyCounter(cache, imageId, data.imageResetO);
    },
  });
  return { incrementO, decrementO, resetO };
}
