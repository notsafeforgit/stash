/**
 * Performer detail tab panel components.
 *
 * Each export is a self-contained panel that receives the performer and renders
 * its content. Tabs are assembled in the route component.
 */

import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { MetaRow } from "src/components/detail/meta-row";
import { CustomFieldsRows } from "src/components/detail/custom-fields-rows";
import { CountryDisplay } from "src/components/forms/country-select";
import { CreatedUpdatedMetaRows } from "src/components/detail/timestamp-meta-rows";

// ── Shared helpers ─────────────────────────────────────────────────────────────

type PerformerData = NonNullable<GQL.FindPerformerQuery["findPerformer"]>;

function formatAge(
  birthdate: string | null | undefined,
  deathDate: string | null | undefined,
): string | null {
  if (!birthdate) return null;
  const birth = new Date(birthdate);
  const ref = deathDate ? new Date(deathDate) : new Date();
  let age = ref.getFullYear() - birth.getFullYear();
  const m = ref.getMonth() - birth.getMonth();
  if (m < 0 || (m === 0 && ref.getDate() < birth.getDate())) age--;
  return String(age);
}

function formatHeight(cm: number | null | undefined): string | null {
  if (cm == null) return null;
  return `${cm} cm`;
}

function formatWeight(kg: number | null | undefined): string | null {
  if (kg == null) return null;
  return `${kg} kg`;
}

function formatCareer(
  start: string | null | undefined,
  end: string | null | undefined,
): string | null {
  if (!start && !end) return null;
  if (start && end) return `${start} – ${end}`;
  if (start) return `${start} –`;
  return `– ${end}`;
}

// ── Details tab ────────────────────────────────────────────────────────────────

export function PerformerDetailsTab({
  performer,
}: {
  performer: PerformerData;
}) {
  const intl = useIntl();
  const age = formatAge(performer.birthdate, performer.death_date);

  const aliases = performer.aliases.map((a) => a.alias).filter(Boolean);

  return (
    <dl className="grid m-0 p-0">
      {performer.disambiguation && (
        <MetaRow
          label={intl.formatMessage({
            id: "disambiguation",
            defaultMessage: "Disambiguation",
          })}
        >
          {performer.disambiguation}
        </MetaRow>
      )}

      {aliases.length > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "aliases",
            defaultMessage: "Aliases",
          })}
        >
          <div className="flex flex-col gap-0.5">
            {aliases.map((alias) => (
              <span key={alias}>{alias}</span>
            ))}
          </div>
        </MetaRow>
      )}

      {performer.gender && (
        <MetaRow
          label={intl.formatMessage({ id: "gender", defaultMessage: "Gender" })}
        >
          {intl.formatMessage({
            id: `gender_types.${performer.gender}`,
            defaultMessage: performer.gender,
          })}
        </MetaRow>
      )}

      {performer.birthdate && (
        <MetaRow
          label={intl.formatMessage({
            id: "birthdate",
            defaultMessage: "Birthdate",
          })}
        >
          {performer.birthdate}
          {age && <span className="text-muted-foreground ml-1">({age})</span>}
        </MetaRow>
      )}

      {performer.death_date && (
        <MetaRow
          label={intl.formatMessage({
            id: "death_date",
            defaultMessage: "Death date",
          })}
        >
          {performer.death_date}
        </MetaRow>
      )}

      {performer.country && (
        <MetaRow
          label={intl.formatMessage({
            id: "country",
            defaultMessage: "Country",
          })}
        >
          <CountryDisplay value={performer.country} />
        </MetaRow>
      )}

      {performer.ethnicity && (
        <MetaRow
          label={intl.formatMessage({
            id: "ethnicity",
            defaultMessage: "Ethnicity",
          })}
        >
          {performer.ethnicity}
        </MetaRow>
      )}

      {performer.hair_color && (
        <MetaRow
          label={intl.formatMessage({
            id: "hair_color",
            defaultMessage: "Hair colour",
          })}
        >
          {performer.hair_color}
        </MetaRow>
      )}

      {performer.eye_color && (
        <MetaRow
          label={intl.formatMessage({
            id: "eye_color",
            defaultMessage: "Eye colour",
          })}
        >
          {performer.eye_color}
        </MetaRow>
      )}

      {performer.height_cm != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "height",
            defaultMessage: "Height",
          })}
        >
          {formatHeight(performer.height_cm)}
        </MetaRow>
      )}

      {performer.weight != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "weight",
            defaultMessage: "Weight",
          })}
        >
          {formatWeight(performer.weight)}
        </MetaRow>
      )}

      {performer.measurements && (
        <MetaRow
          label={intl.formatMessage({
            id: "measurements",
            defaultMessage: "Measurements",
          })}
        >
          {performer.measurements}
        </MetaRow>
      )}

      {performer.penis_length != null && (
        <MetaRow
          label={intl.formatMessage({
            id: "penis_length",
            defaultMessage: "Penis length",
          })}
        >
          {performer.penis_length} cm
        </MetaRow>
      )}

      {performer.circumcised && (
        <MetaRow
          label={intl.formatMessage({
            id: "circumcised",
            defaultMessage: "Circumcised",
          })}
        >
          {intl.formatMessage({
            id: `circumcised_types.${performer.circumcised}`,
            defaultMessage: performer.circumcised,
          })}
        </MetaRow>
      )}

      {performer.fake_tits && (
        <MetaRow
          label={intl.formatMessage({
            id: "fake_tits",
            defaultMessage: "Fake tits",
          })}
        >
          {performer.fake_tits}
        </MetaRow>
      )}

      {performer.tattoos && (
        <MetaRow
          label={intl.formatMessage({
            id: "tattoos",
            defaultMessage: "Tattoos",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{performer.tattoos}</p>
        </MetaRow>
      )}

      {performer.piercings && (
        <MetaRow
          label={intl.formatMessage({
            id: "piercings",
            defaultMessage: "Piercings",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{performer.piercings}</p>
        </MetaRow>
      )}

      {formatCareer(performer.career_start, performer.career_end) && (
        <MetaRow
          label={intl.formatMessage({
            id: "career_length",
            defaultMessage: "Career",
          })}
        >
          {formatCareer(performer.career_start, performer.career_end)}
        </MetaRow>
      )}

      {performer.details && (
        <MetaRow
          label={intl.formatMessage({
            id: "details",
            defaultMessage: "Details",
          })}
        >
          <p className="m-0 whitespace-pre-wrap">{performer.details}</p>
        </MetaRow>
      )}

      {(performer.urls ?? []).length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "url", defaultMessage: "URL" })}
        >
          <div className="flex flex-col gap-1">
            {(performer.urls ?? []).map((url) => (
              <a
                key={url}
                href={url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-primary no-underline hover:underline"
              >
                {url}
              </a>
            ))}
          </div>
        </MetaRow>
      )}

      {performer.tags.length > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
        >
          <div className="flex flex-wrap gap-1">
            {performer.tags.map((t) => (
              <Badge
                key={t.id}
                variant="secondary"
                render={<Link to="/tags/$tagId" params={{ tagId: t.id }} />}
              >
                {t.name}
              </Badge>
            ))}
          </div>
        </MetaRow>
      )}

      {performer.rating100 != null && (
        <MetaRow
          label={intl.formatMessage({ id: "rating", defaultMessage: "Rating" })}
        >
          {performer.rating100}
        </MetaRow>
      )}

      <MetaRow
        label={intl.formatMessage({ id: "scenes", defaultMessage: "Scenes" })}
      >
        {performer.scene_count}
      </MetaRow>

      {performer.image_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "images", defaultMessage: "Images" })}
        >
          {performer.image_count}
        </MetaRow>
      )}

      {performer.gallery_count > 0 && (
        <MetaRow
          label={intl.formatMessage({
            id: "galleries",
            defaultMessage: "Galleries",
          })}
        >
          {performer.gallery_count}
        </MetaRow>
      )}

      {performer.group_count > 0 && (
        <MetaRow
          label={intl.formatMessage({ id: "groups", defaultMessage: "Groups" })}
        >
          {performer.group_count}
        </MetaRow>
      )}
      <CreatedUpdatedMetaRows
        createdAt={performer.created_at}
        updatedAt={performer.updated_at}
      />
      <CustomFieldsRows values={performer.custom_fields} />
    </dl>
  );
}
