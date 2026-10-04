import { MockedProvider } from "@apollo/client/testing/react";
import { useSearch, useNavigate } from "@tanstack/react-router";
import { FindPerformersForSelectDocument } from "@/core/generated-graphql";
import { AccountReview } from "@/components/archive/account-review";
import { BottomTabBar } from "@/components/layout/bottom-tab-bar";
import { UserMenu } from "@/components/layout/user-menu";

export function AccountReviewFixture() {
  const { account, ...filter } = useSearch({ from: "/account-review" });
  const navigate = useNavigate({ from: "/account-review" });
  return (
    <MockedProvider
      mocks={[
        {
          request: {
            query: FindPerformersForSelectDocument,
            variables: { filter: { q: "River", per_page: 25, page: 1 } },
          },
          maxUsageCount: Infinity,
          result: {
            data: {
              findPerformers: {
                __typename: "FindPerformersResultType",
                count: 2,
                performers: [10, 11].map((id) => ({
                  __typename: "Performer",
                  id: String(id),
                  name: "River",
                  disambiguation: id === 10 ? "Model" : "Photographer",
                  aliases: ["river"],
                  image_path: "",
                  birthdate: null,
                  death_date: null,
                })),
              },
            },
          },
        },
      ]}
    >
      {/* biome-ignore lint/complexity/noUselessFragments: Apollo MockedProvider only renders a single React element. */}
      <>
        <div className="hidden justify-end p-2 md:flex">
          <UserMenu />
        </div>
        <AccountReview
          key={JSON.stringify(filter)}
          filter={filter}
          selected={account}
          onFilterChange={(next) => void navigate({ search: next })}
          onSelect={(account) =>
            void navigate({ search: { ...filter, account } })
          }
        />
        <BottomTabBar />
      </>
    </MockedProvider>
  );
}
