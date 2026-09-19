import {
  type ErrorComponentProps,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { useSmartBack } from "@/hooks/use-smart-back";
import { QueryError } from "@/components/query-error";
import { CollectionDetailLayout } from "./collection-detail-layout";
import { DetailPageSkeleton } from "./detail-page-skeleton";
import { DetailSidebarBack } from "./detail-page-parts";
import { MediaDetailLayout } from "./media-detail-layout";
import { Skeleton } from "@/components/ui/skeleton";

function DetailRoutePending() {
  const goBack = useSmartBack("/");
  return (
    <CollectionDetailLayout title="" onBack={goBack}>
      <DetailPageSkeleton
        imageAspect="aspect-[2/3]"
        navigation={<DetailSidebarBack onBack={goBack} />}
      />
    </CollectionDetailLayout>
  );
}

export function SceneRoutePending() {
  const goBack = useSmartBack("/scenes");
  return (
    <MediaDetailLayout
      primaryContent={<Skeleton className="aspect-video w-full" />}
      headerContent={<Skeleton className="h-8 w-2/3" />}
      tabs={[]}
      activeTab=""
      onTabChange={() => {}}
      onBack={goBack}
      mobilePageScroll
    />
  );
}

function DetailRouteError({ error, reset }: ErrorComponentProps) {
  const router = useRouter();
  const retrying = useRouterState({ select: (s) => s.isLoading });
  const goBack = useSmartBack("/");
  return (
    <CollectionDetailLayout title="" onBack={goBack}>
      <DetailSidebarBack onBack={goBack} />
      <QueryError
        error={error instanceof Error ? error : new Error(String(error))}
        retrying={retrying}
        retry={async () => {
          await router.invalidate();
          reset();
        }}
      />
    </CollectionDetailLayout>
  );
}

/** Retain the outgoing view during short loads; slow loads remain navigable. */
export const detailRouteState = {
  pendingMs: 600,
  pendingMinMs: 0,
  pendingComponent: DetailRoutePending,
  errorComponent: DetailRouteError,
};
