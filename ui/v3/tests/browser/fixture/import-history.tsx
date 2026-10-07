import { useNavigate, useSearch } from "@tanstack/react-router";
import { ImportHistory } from "@/components/archive/import-history";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { AssociationFixtureProvider } from "./association-provider";

export function ImportHistoryFixture() {
  const search = useSearch({ from: "/import-history" });
  const navigate = useNavigate({ from: "/import-history" });
  return (
    <AssociationFixtureProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <ImportHistory
          key={search.kind}
          search={search}
          onChange={(search) => void navigate({ search })}
        />
        <BottomTabBar />
      </div>
    </AssociationFixtureProvider>
  );
}
