import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArchiveActivity } from "@/components/archive/activity";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { AssociationFixtureProvider } from "./association-provider";

export function ArchiveActivityFixture() {
  const search = useSearch({ from: "/archive-activity" });
  const { item: _, ...filter } = search;
  const navigate = useNavigate({ from: "/archive-activity" });
  return (
    <AssociationFixtureProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <ArchiveActivity
          key={JSON.stringify(filter)}
          search={search}
          onChange={(search) => void navigate({ search })}
        />
        <BottomTabBar />
      </div>
    </AssociationFixtureProvider>
  );
}
