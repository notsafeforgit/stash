import { useNavigate, useSearch } from "@tanstack/react-router";
import { SavedActions } from "@/components/archive/saved-actions";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";
import { AssociationFixtureProvider } from "./association-provider";

export function SavedActionsFixture() {
  const { family } = useSearch({ from: "/saved-actions" });
  const navigate = useNavigate({ from: "/saved-actions" });
  return (
    <AssociationFixtureProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <SavedActions
          key={family}
          family={family}
          onFamilyChange={(family) => void navigate({ search: { family } })}
        />
        <BottomTabBar />
      </div>
    </AssociationFixtureProvider>
  );
}
