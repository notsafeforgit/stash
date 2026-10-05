import { MockedProvider } from "@apollo/client/testing/react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { MediaRoots } from "@/components/archive/media-roots";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";

export function MediaRootsFixture() {
  const { root, create, ...filter } = useSearch({ from: "/media-roots" });
  const navigate = useNavigate({ from: "/media-roots" });
  return (
    <MockedProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <MediaRoots
          key={JSON.stringify(filter)}
          filter={filter}
          selected={root}
          create={create}
          onFilterChange={(filter) => void navigate({ search: filter })}
          onSelect={(root, create) =>
            void navigate({ search: { ...filter, root, create } })
          }
        />
        <BottomTabBar />
      </div>
    </MockedProvider>
  );
}
