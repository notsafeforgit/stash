import { useMemo, useState } from "react";
import { InMemoryCache } from "@apollo/client";
import { MockedProvider } from "@apollo/client/testing/react";
import type { MockedResponse } from "@apollo/client/testing";
import { Lightbox, type LightboxSlide } from "@/components/lightbox/lightbox";
import { Button } from "@/components/ui/button";
import { ConfigurationProvider } from "@/hooks/config";
import * as GQL from "@/core/generated-graphql";
import { playerConfiguration } from "./player-configuration";
import { RatingStarPrecision, RatingSystemType } from "@/utils/rating";

const artwork = `data:image/svg+xml,${encodeURIComponent(
  '<svg xmlns="http://www.w3.org/2000/svg" width="900" height="1200"><rect width="900" height="1200" fill="#264653"/><circle cx="450" cy="400" r="250" fill="#2a9d8f"/><path d="M0 1200L450 600L900 1200Z" fill="#e9c46a"/></svg>',
)}`;

export const images: GQL.SlimImageDataFragment[] = ["1", "2"].map((id) => ({
  __typename: "Image",
  id,
  title: `Image ${id}: ${"A long image title without room to fit ".repeat(6)}`,
  code: null,
  date: "2026-09-17",
  urls: [],
  details: "An image description with a long URL: " + "long-word-".repeat(40),
  photographer: null,
  rating100: 80,
  organized: false,
  o_counter: 9999,
  paths: {
    __typename: "ImagePathsType",
    thumbnail: artwork,
    preview: artwork,
    image: artwork,
  },
  galleries: Array.from({ length: 8 }, (_, i) => ({
    __typename: "Gallery",
    id: `gallery-${i}`,
    title: `Gallery ${i}: ${"A long gallery name ".repeat(4)}`,
    files: [],
    folder: null,
  })),
  studio: null,
  tags: [],
  performers: [
    {
      __typename: "Performer",
      id: "performer",
      name: "A performer name that also needs to fit the narrow viewport",
      favorite: false,
      gender: null,
      image_path: null,
    },
  ],
  visual_files: [
    {
      __typename: "ImageFile",
      id: `file-${id}`,
      path: `/fixture/image-${id}.svg`,
      size: 1000,
      mod_time: "2026-09-17",
      width: 900,
      height: 1200,
      bit_depth: 8,
      color_range: null,
      color_space: null,
      color_transfer: null,
      color_primaries: null,
      fingerprints: [],
    },
  ],
}));

const increment: MockedResponse<
  GQL.ImageIncrementOMutation,
  GQL.ImageIncrementOMutationVariables
> = {
  request: { query: GQL.ImageIncrementODocument, variables: { id: "1" } },
  result: { data: { imageIncrementO: 10000 } },
};

export function ImageLightboxFixture() {
  const [open, setOpen] = useState(false);
  const [index, setIndex] = useState(0);
  const params = new URLSearchParams(location.search);
  const cache = useMemo(() => {
    const cache = new InMemoryCache();
    for (const image of images) {
      cache.writeFragment({
        id: cache.identify(image),
        fragment: GQL.SlimImageDataFragmentDoc,
        fragmentName: "SlimImageData",
        data: image,
      });
    }
    return cache;
  }, []);
  const slides: LightboxSlide[] = images
    .slice(0, params.has("single") ? 1 : 2)
    .map((image) => ({
      src: artwork,
      width: 900,
      height: 1200,
      imageId: image.id,
      imageTitle: image.title ?? "",
      filePaths: image.visual_files.map((file) => file.path),
    }));
  const configuration = {
    ...playerConfiguration,
    ui: {
      ratingSystemOptions: {
        type: RatingSystemType.Stars,
        starPrecision: params.has("fractional")
          ? RatingStarPrecision.Half
          : RatingStarPrecision.Full,
      },
    },
  };
  return (
    <MockedProvider cache={cache} mocks={[increment]}>
      <ConfigurationProvider configuration={configuration}>
        <Button onClick={() => setOpen(true)}>Open images</Button>
        <Lightbox
          open={open}
          onClose={() => setOpen(false)}
          slides={slides}
          index={index}
          onView={setIndex}
          onDeleteImage={() => Promise.resolve()}
        />
      </ConfigurationProvider>
    </MockedProvider>
  );
}
