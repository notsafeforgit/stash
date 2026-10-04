import { createFileRoute } from "@tanstack/react-router";
import { useDocumentTitle } from "@/hooks/title";
import { useMsg } from "@/hooks/message";
import { AccountReview } from "@/components/archive/account-review";
import {
  accountFilterSchema,
  accountUUIDSchema,
} from "@/core/native-archive/account-review-api";

const searchSchema = accountFilterSchema.extend({
  account: accountUUIDSchema.optional(),
});
function AccountReviewPage() {
  const msg = useMsg();
  useDocumentTitle(msg("account_review.title", "Account review"));
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const { account, ...filter } = search;
  return (
    <AccountReview
      key={JSON.stringify(filter)}
      filter={filter}
      selected={account}
      onFilterChange={(filter) => void navigate({ search: filter })}
      onSelect={(account) => void navigate({ search: { ...filter, account } })}
    />
  );
}
export const Route = createFileRoute("/account-review")({
  validateSearch: searchSchema,
  component: AccountReviewPage,
});
