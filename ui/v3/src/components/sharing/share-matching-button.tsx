import { useEffect, useRef, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { useIntl } from "react-intl";
import { ListChecks } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useToast } from "@/hooks/toast";
import { imageTitle, objectTitle } from "@/core/files";
import { galleryLabel } from "@/lib/gallery-utils";
import {
  ShareEntityKind,
  ShareMatchingTargetsDocument,
} from "@/core/generated-graphql";
import type { ListFilterModel } from "@/models/list-filter/filter";
import { MAX_SHARE_TARGETS, type ShareSelection } from "./share-target-picker";

export interface MatchingShareSelection {
  kind: ShareEntityKind;
  filter: ListFilterModel;
}

export function ShareMatchingButton({
  matching,
  onSelect,
  onLoadingChange,
}: {
  matching: MatchingShareSelection;
  onSelect: (targets: ShareSelection[]) => void;
  onLoadingChange: (loading: boolean) => void;
}) {
  const client = useApolloClient();
  const intl = useIntl();
  const toast = useToast();
  const [loading, setLoading] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);

  async function selectMatching() {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setLoading(true);
    onLoadingChange(true);
    try {
      const { data } = await client.query({
        query: ShareMatchingTargetsDocument,
        variables: {
          filter: {
            ...matching.filter.makeFindFilter(),
            page: 1,
            per_page: MAX_SHARE_TARGETS + 1,
          },
          filter_ast: matching.filter.makeFilterAST(),
          scenes: matching.kind === ShareEntityKind.Scene,
          images: matching.kind === ShareEntityKind.Image,
          galleries: matching.kind === ShareEntityKind.Gallery,
        },
        fetchPolicy: "no-cache",
        context: { fetchOptions: { signal: controller.signal } },
      });
      if (controller.signal.aborted) return;
      const result =
        data?.findScenes ?? data?.findImages ?? data?.findGalleries;
      if (!data || !result)
        throw new Error(
          intl.formatMessage({
            id: "sharing.matching_failed",
            defaultMessage: "Unable to load all matching items. Try again.",
          }),
        );
      const limit = Math.min(
        MAX_SHARE_TARGETS,
        data.sharingConfiguration.max_items,
      );
      if (result.count > limit)
        throw new Error(
          intl.formatMessage(
            {
              id: "sharing.matching_limit",
              defaultMessage:
                "A share can include at most {limit, number} items. Narrow the filters or share a smaller selection.",
            },
            { limit },
          ),
        );
      const targets: ShareSelection[] = [
        ...(data.findScenes?.scenes.map((item) => ({
          kind: ShareEntityKind.Scene,
          id: item.id,
          name: objectTitle(item) || item.id,
        })) ?? []),
        ...(data.findImages?.images.map((item) => ({
          kind: ShareEntityKind.Image,
          id: item.id,
          name: imageTitle(item) || item.id,
        })) ?? []),
        ...(data.findGalleries?.galleries.map((item) => ({
          kind: ShareEntityKind.Gallery,
          id: item.id,
          name: galleryLabel(item) || item.id,
        })) ?? []),
      ];
      // Never silently replace the user's selection with a partial result.
      if (targets.length !== result.count)
        throw new Error(
          intl.formatMessage({
            id: "sharing.matching_failed",
            defaultMessage: "Unable to load all matching items. Try again.",
          }),
        );
      onSelect(targets);
    } catch (error) {
      if (!controller.signal.aborted) toast.error(error);
    } finally {
      if (!controller.signal.aborted) {
        request.current = null;
        setLoading(false);
        onLoadingChange(false);
      }
    }
  }

  return (
    <Button
      variant="outline"
      className="h-auto min-h-11 whitespace-normal"
      disabled={loading}
      onClick={() => void selectMatching()}
    >
      {loading ? <Spinner /> : <ListChecks data-icon="inline-start" />}
      {intl.formatMessage({
        id: "sharing.use_all_matching",
        defaultMessage: "Use all matching items across pages",
      })}
    </Button>
  );
}
