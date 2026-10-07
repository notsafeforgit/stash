import { useNavigate, useSearch } from "@tanstack/react-router";
import { ReviewQueue } from "@/components/archive/review-queue";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { AssociationFixtureProvider } from "./association-provider";

export function ReviewQueueFixture() {
  const search = useSearch({ from: "/review-queue" });
  const navigate = useNavigate({ from: "/review-queue" });
  return (
    <AssociationFixtureProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <ReviewQueue
          key={search.kind}
          search={search}
          onChange={(search) => void navigate({ search })}
        />
        <BottomTabBar />
      </div>
    </AssociationFixtureProvider>
  );
}
