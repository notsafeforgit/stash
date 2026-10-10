import { useCallback } from "react";
import { useMutation } from "@apollo/client/react";
import { toast } from "sonner";
import {
  FindGalleryCoverDocument,
  SetGalleryCoverDocument,
} from "@/core/generated-graphql";
import { useMsg } from "@/hooks/message";

export function useGalleryCover(galleryId: string) {
  const msg = useMsg();
  const [mutate, { loading }] = useMutation(SetGalleryCoverDocument, {
    refetchQueries: [
      { query: FindGalleryCoverDocument, variables: { id: galleryId } },
    ],
    awaitRefetchQueries: true,
  });
  const setCover = useCallback(
    async (kind: "image" | "scene", id: string) => {
      try {
        await mutate({
          variables: {
            gallery_id: galleryId,
            image_id: kind === "image" ? id : null,
            scene_id: kind === "scene" ? id : null,
          },
        });
      } catch {
        toast.error(
          msg(
            "gallery_media.cover_failed",
            "Could not set the gallery cover. Refresh the gallery and try again.",
          ),
        );
      }
    },
    [galleryId, mutate, msg],
  );
  return { setCover, pending: loading };
}
