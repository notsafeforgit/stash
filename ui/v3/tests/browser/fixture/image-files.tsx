import { useMemo } from "react";
import { InMemoryCache } from "@apollo/client";
import { MockedProvider } from "@apollo/client/testing/react";
import { ImageCard } from "@/components/cards/image-card";
import { ImageRowContextMenu } from "@/components/cards/use-image-context-menu";
import { ImageActionsMenu } from "@/components/detail/image-actions-menu";
import { ImageViewer } from "@/components/detail/image-viewer";
import { Toaster } from "@/components/ui/sonner";
import { ConfigurationProvider } from "@/hooks/config";
import * as GQL from "@/core/generated-graphql";
import { images } from "./image-lightbox";
import { playerConfiguration } from "./player-configuration";

const base = images[0];
if (!base) throw new Error("Missing image fixture");
const image: GQL.ImageDataFragment = {
  ...base,
  title: "Original image",
  galleries: [],
  performers: [],
  studio: null,
  tags: [],
  created_at: "2026-09-18",
  updated_at: "2026-09-18",
  custom_fields: {},
};

export function ImageFilesFixture() {
  const cache = useMemo(() => {
    const cache = new InMemoryCache();
    cache.writeQuery({
      query: GQL.FindImageDocument,
      variables: { id: image.id },
      data: { findImage: image },
    });
    return cache;
  }, []);
  return (
    <MockedProvider cache={cache}>
      <ConfigurationProvider configuration={playerConfiguration}>
        <Toaster />
        <div className="p-3">
          <div data-testid="image-card" className="w-48">
            <ImageCard image={image} onPreviewClick={() => {}} />
          </div>
          <table>
            <tbody>
              <ImageRowContextMenu image={image}>
                <tr data-testid="image-row">
                  <td>Image table row</td>
                </tr>
              </ImageRowContextMenu>
            </tbody>
          </table>
          <ImageActionsMenu image={image} />
          <div className="h-96">
            <ImageViewer image={image} />
          </div>
        </div>
      </ConfigurationProvider>
    </MockedProvider>
  );
}
