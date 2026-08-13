import { ExternalLinkIcon, EyeIcon, TriangleAlertIcon } from "lucide-react";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "src/components/ui/popover";
import { getStashboxBase } from "src/utils/stashbox";
import type { PerformerCollision } from "./performer-match";

interface PreviewData {
  name: string;
  disambiguation?: string | null;
  image?: string | null;
  birthdate?: string | null;
  country?: string | null;
  ethnicity?: string | null;
  gender?: string | null;
  href?: string;
}

function PerformerPreviewCard({ performer }: { performer: PreviewData }) {
  const intl = useIntl();
  const rows = [
    [
      intl.formatMessage({ id: "birthdate", defaultMessage: "Birthdate" }),
      performer.birthdate,
    ],
    [
      intl.formatMessage({ id: "country", defaultMessage: "Country" }),
      performer.country,
    ],
    [
      intl.formatMessage({ id: "ethnicity", defaultMessage: "Ethnicity" }),
      performer.ethnicity,
    ],
    [
      intl.formatMessage({ id: "gender", defaultMessage: "Gender" }),
      performer.gender,
    ],
  ].filter((row): row is [string, string] => !!row[1]);

  return (
    <div className="grid grid-cols-[5rem_1fr] gap-3">
      <div className="aspect-[2/3] overflow-hidden rounded-md bg-muted">
        {performer.image && (
          <img
            src={performer.image}
            alt=""
            className="size-full object-cover"
            loading="lazy"
          />
        )}
      </div>
      <div className="min-w-0">
        <div className="font-medium break-words">
          {performer.name}
          {performer.disambiguation && (
            <span className="text-muted-foreground">
              {` (${performer.disambiguation})`}
            </span>
          )}
        </div>
        {rows.length > 0 && (
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-xs">
            {rows.map(([label, value]) => (
              <div key={label} className="contents">
                <dt className="text-muted-foreground">{label}</dt>
                <dd className="truncate" title={value}>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {performer.href && (
          <a
            href={performer.href}
            target="_blank"
            rel="noreferrer"
            className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline"
          >
            View performer
            <ExternalLinkIcon className="size-3" />
          </a>
        )}
      </div>
    </div>
  );
}

function PreviewButton({
  label,
  performer,
}: {
  label: string;
  performer: PreviewData;
}) {
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="size-6 shrink-0"
            aria-label={label}
            title={label}
          />
        }
      >
        <EyeIcon className="size-3.5" />
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="start"
        className="w-72"
        positionerClassName="z-[60]"
      >
        <PerformerPreviewCard performer={performer} />
      </PopoverContent>
    </Popover>
  );
}

export function ScrapedPerformerPreviewButton({
  performer,
  endpoint,
}: {
  performer: GQL.ScrapedScenePerformerDataFragment;
  endpoint?: string;
}) {
  const intl = useIntl();
  const base = endpoint ? getStashboxBase(endpoint) : undefined;
  const href =
    base && performer.remote_site_id
      ? `${base}performers/${performer.remote_site_id}`
      : undefined;

  return (
    <PreviewButton
      label={intl.formatMessage({
        id: "scrape.preview_scraped_performer",
        defaultMessage: "Preview scraped performer",
      })}
      performer={{
        name: performer.name ?? "",
        disambiguation: performer.disambiguation,
        image: performer.images?.[0],
        birthdate: performer.birthdate,
        country: performer.country,
        ethnicity: performer.ethnicity,
        gender: performer.gender,
        href,
      }}
    />
  );
}

export function LocalPerformerPreviewButton({
  performer,
}: {
  performer: GQL.PerformerDataFragment;
}) {
  const intl = useIntl();
  return (
    <PreviewButton
      label={intl.formatMessage({
        id: "scrape.preview_matched_performer",
        defaultMessage: "Preview matched performer",
      })}
      performer={{
        name: performer.name,
        disambiguation: performer.disambiguation,
        image: performer.image_path,
        birthdate: performer.birthdate,
        country: performer.country,
        ethnicity: performer.ethnicity,
        gender: performer.gender,
        href: `/performers/${performer.id}`,
      }}
    />
  );
}

const COLLISION_MESSAGES: Record<
  PerformerCollision,
  { id: string; defaultMessage: string }
> = {
  stash_mismatch: {
    id: "scrape.performer_collision_stash_mismatch",
    defaultMessage: "Matched performer already has a different stash-id.",
  },
  birthdate: {
    id: "scrape.performer_collision_birthdate",
    defaultMessage: "Matched performer has a different birthdate.",
  },
  country: {
    id: "scrape.performer_collision_country",
    defaultMessage: "Matched performer is from a different country.",
  },
  ethnicity: {
    id: "scrape.performer_collision_ethnicity",
    defaultMessage: "Matched performer has a different ethnicity.",
  },
  gender: {
    id: "scrape.performer_collision_gender",
    defaultMessage: "Matched performer has a different gender.",
  },
};

export function PerformerCollisionWarning({
  collisions,
}: {
  collisions: PerformerCollision[];
}) {
  const intl = useIntl();
  if (collisions.length === 0) return null;

  const label = intl.formatMessage({
    id: "scrape.performer_collision_warning",
    defaultMessage: "Matched performer has conflicting identity data",
  });

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="size-7 shrink-0 text-amber-500 hover:text-amber-500"
            aria-label={label}
            title={label}
          />
        }
      >
        <TriangleAlertIcon className="size-4" />
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        className="w-80"
        positionerClassName="z-[60]"
      >
        <ul className="flex flex-col gap-2">
          {collisions.map((collision) => {
            const message = COLLISION_MESSAGES[collision];
            return (
              <li key={collision} className="flex items-start gap-2 text-sm">
                <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-amber-500" />
                <span>{intl.formatMessage(message)}</span>
              </li>
            );
          })}
        </ul>
      </PopoverContent>
    </Popover>
  );
}
