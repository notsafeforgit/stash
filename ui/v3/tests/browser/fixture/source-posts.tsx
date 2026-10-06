import { MockedProvider } from "@apollo/client/testing/react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { SourcePosts } from "@/components/archive/source-posts";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";

export function SourcePostsFixture() {
  const { post, ...filter } = useSearch({ from: "/source-posts" });
  const navigate = useNavigate({ from: "/source-posts" });
  return (
    <MockedProvider>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <SourcePosts
          key={JSON.stringify(filter)}
          filter={filter}
          selected={post}
          onFilterChange={(filter) => void navigate({ search: filter })}
          onSelect={(post) => void navigate({ search: { ...filter, post } })}
        />
        <BottomTabBar />
      </div>
    </MockedProvider>
  );
}
