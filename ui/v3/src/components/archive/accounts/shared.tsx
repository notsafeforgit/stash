import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  Account,
  AccountOwnership,
  AccountPerformer,
} from "@/core/native-archive/account-review-api";
import { Badge } from "@/components/ui/badge";

const services: Record<string, string> = {
  reddit: "Reddit",
  twitter: "Twitter",
  bluesky: "Bluesky",
  tiktok: "TikTok",
  instagram: "Instagram",
  patreon: "Patreon",
  onlyfans: "OnlyFans",
  fansly: "Fansly",
  coomer: "Coomer",
  kemono: "Kemono",
};
export function AccountService({ namespace }: { namespace: string }) {
  const intl = useIntl();
  const parts = namespace.split(":");
  const [kind, service = "", mirrorService = ""] = parts;
  function label(service: string) {
    return services[service]
      ? intl.formatMessage({
          id: `account_review.services.${service}`,
          defaultMessage: services[service],
        })
      : service;
  }
  const value =
    kind === "native" && parts.length === 2
      ? label(service)
      : kind === "mirror" && parts.length === 3
        ? `${label(service)} · ${label(mirrorService)}`
        : namespace;
  return <Badge variant="secondary">{value}</Badge>;
}
export function AccountName({ account }: { account: Account }) {
  const msg = useMsg();
  return (
    account.label ||
    account.identifiers.find((item) => item.reference.kind === "handle")
      ?.reference.value ||
    msg("account_review.unnamed", "Unlabelled account")
  );
}
export function PerformerName({ performer }: { performer: AccountPerformer }) {
  const msg = useMsg();
  if (performer.state !== "active" || !performer.local_id)
    return (
      <span>
        {msg(
          "account_review.retired_performer",
          "Linked performer is no longer available",
        )}
      </span>
    );
  return (
    <Link
      className="underline underline-offset-4"
      to="/performers/$performerId"
      params={{ performerId: String(performer.local_id) }}
    >
      {performer.name}
      {performer.disambiguation ? ` (${performer.disambiguation})` : ""} (#
      {performer.local_id})
    </Link>
  );
}
export function AccountOwner({
  ownership,
  tracked,
}: {
  ownership?: AccountOwnership;
  tracked: boolean;
}) {
  const msg = useMsg();
  if (ownership?.state === "linked" && ownership.performer)
    return <PerformerName performer={ownership.performer} />;
  if (!tracked)
    return (
      <span>{msg("account_review.incidental", "Incidental post author")}</span>
    );
  return (
    <span>
      {ownership?.state === "unlinked"
        ? msg("account_review.unlinked", "Explicitly unlinked")
        : msg("account_review.undecided", "Needs review")}
    </span>
  );
}

export function AccountOrigin({ origin }: { origin: string }) {
  const msg = useMsg();
  switch (origin) {
    case "review":
      return msg("account_review.origin_review", "Reviewed in Stash");
    case "migration":
      return msg("account_review.origin_migration", "Imported catalog history");
    case "gallery-dl":
      return "gallery-dl";
    default:
      return origin;
  }
}

export function AccountEvidenceBasis({ basis }: { basis: string }) {
  const msg = useMsg();
  switch (basis) {
    case "captured-author":
      return msg("account_review.basis_author", "Captured author");
    case "captured-mirror-public-id":
      return msg("account_review.basis_mirror", "Captured mirror profile");
    case "legacy-locator":
      return msg("account_review.basis_locator", "Retained catalog locator");
    case "profile-url":
      return msg("account_review.basis_profile", "Performer profile URL");
    case "explicit":
      return msg("account_review.basis_explicit", "Explicit association");
    default:
      return basis;
  }
}
