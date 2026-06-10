import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Checkbox } from "src/components/ui/checkbox";
import { Label } from "src/components/ui/label";
import { TagIdPicker } from "src/components/shared/tag-id-picker";
import { RadioGroup, RadioGroupItem } from "src/components/ui/radio-group";
import { ALL_GENDERS } from "./identify-types";
import { IdentifyFieldOptionsTable } from "./identify-field-options-table";

interface IProps {
  options: GQL.IdentifyMetadataOptionsInput;
  setOptions: (s: GQL.IdentifyMetadataOptionsInput) => void;
  /** When set, this editor is for a single source and may render "Use default". */
  source?: { displayName: string };
  /** Default options to label "Use default (…)" inheritance hints. */
  defaultOptions?: GQL.IdentifyMetadataOptionsInput;
}

/** Tri-state radio: undefined ("use default") / true / false. */
function TriBoolean({
  id,
  label,
  description,
  value,
  allowDefault,
  defaultValue,
  onChange,
}: {
  id: string;
  label: React.ReactNode;
  description?: React.ReactNode;
  value: boolean | null | undefined;
  allowDefault: boolean;
  defaultValue?: boolean | null;
  onChange: (v: boolean | undefined) => void;
}) {
  const intl = useIntl();
  if (!allowDefault) {
    return (
      <div className="flex items-start gap-2">
        <Checkbox
          id={id}
          checked={value ?? false}
          onCheckedChange={(v) => onChange(v === true)}
          className="mt-0.5"
        />
        <div>
          <Label htmlFor={id}>{label}</Label>
          {description && (
            <p className="text-xs text-muted-foreground">{description}</p>
          )}
        </div>
      </div>
    );
  }

  const v =
    value === null || value === undefined
      ? "default"
      : value
        ? "true"
        : "false";

  return (
    <div className="space-y-1">
      <div className="text-sm font-medium">{label}</div>
      {description && (
        <p className="text-xs text-muted-foreground">{description}</p>
      )}
      <RadioGroup
        value={v}
        onValueChange={(nv) =>
          onChange(nv === "default" ? undefined : nv === "true")
        }
      >
        <div className="flex items-center gap-2">
          <RadioGroupItem id={`${id}-default`} value="default" />
          <Label htmlFor={`${id}-default`} className="font-normal">
            {intl.formatMessage({
              id: "actions.use_default",
              defaultMessage: "Use default",
            })}
            {typeof defaultValue === "boolean"
              ? ` (${defaultValue ? "yes" : "no"})`
              : ""}
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem id={`${id}-true`} value="true" />
          <Label htmlFor={`${id}-true`} className="font-normal">
            Yes
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem id={`${id}-false`} value="false" />
          <Label htmlFor={`${id}-false`} className="font-normal">
            No
          </Label>
        </div>
      </RadioGroup>
    </div>
  );
}

export function IdentifyOptionsEditor({
  options,
  setOptions,
  source,
  defaultOptions,
}: IProps) {
  const intl = useIntl();
  const allowDefault = !!source;

  function set(input: Partial<GQL.IdentifyMetadataOptionsInput>) {
    setOptions({ ...options, ...input });
  }

  const genders = options.performerGenders ?? ALL_GENDERS;

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-semibold">
          {source
            ? intl.formatMessage(
                {
                  id: "config.tasks.identify.source_options",
                  defaultMessage: "{source} Options",
                },
                { source: source.displayName },
              )
            : intl.formatMessage({
                id: "config.tasks.identify.default_options",
                defaultMessage: "Default options",
              })}
        </h3>
        {!source && (
          <p className="text-xs text-muted-foreground">
            {intl.formatMessage({
              id: "config.tasks.identify.explicit_set_description",
              defaultMessage:
                "These defaults apply where a source-specific override is not set.",
            })}
          </p>
        )}
      </div>

      <div className="space-y-2">
        <Label>
          {intl.formatMessage({
            id: "config.tasks.identify.performer_genders",
            defaultMessage: "Performer genders",
          })}
        </Label>
        <p className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "config.tasks.identify.performer_genders_desc",
            defaultMessage:
              "Performers with selected genders will be included during identification.",
          })}
        </p>
        {source && (
          <div className="flex items-start gap-2">
            <Checkbox
              id="performer-genders-use-default"
              checked={options.performerGenders == null}
              onCheckedChange={(v) => {
                if (v === true) {
                  set({ performerGenders: undefined });
                } else {
                  set({
                    performerGenders:
                      defaultOptions?.performerGenders ?? ALL_GENDERS,
                  });
                }
              }}
              className="mt-0.5"
            />
            <Label
              htmlFor="performer-genders-use-default"
              className="font-normal"
            >
              {intl.formatMessage({
                id: "actions.use_default",
                defaultMessage: "Use default",
              })}
            </Label>
          </div>
        )}
        {(options.performerGenders != null || !source) && (
          <div className="grid grid-cols-2 gap-1">
            {ALL_GENDERS.map((g) => {
              const checked = genders.includes(g);
              return (
                <div key={g} className="flex items-center gap-2">
                  <Checkbox
                    id={`identify-gender-${g}`}
                    checked={checked}
                    onCheckedChange={(v) => {
                      const isChecked = v === true;
                      set({
                        performerGenders: isChecked
                          ? [...genders, g]
                          : genders.filter((x) => x !== g),
                      });
                    }}
                  />
                  <Label
                    htmlFor={`identify-gender-${g}`}
                    className="font-normal"
                  >
                    {intl.formatMessage({
                      id: `gender_types.${g}`,
                      defaultMessage: g,
                    })}
                  </Label>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <TriBoolean
        id="set-cover-image"
        label={intl.formatMessage({
          id: "config.tasks.identify.set_cover_images",
          defaultMessage: "Set cover image",
        })}
        value={options.setCoverImage}
        allowDefault={allowDefault}
        defaultValue={defaultOptions?.setCoverImage}
        onChange={(v) => set({ setCoverImage: v })}
      />

      <TriBoolean
        id="set-organized"
        label={intl.formatMessage({
          id: "config.tasks.identify.set_organized",
          defaultMessage: "Set organised flag",
        })}
        value={options.setOrganized}
        allowDefault={allowDefault}
        defaultValue={defaultOptions?.setOrganized}
        onChange={(v) => set({ setOrganized: v })}
      />

      <TriBoolean
        id="skip-multiple-matches"
        label={intl.formatMessage({
          id: "config.tasks.identify.skip_multiple_matches",
          defaultMessage: "Skip matches that have more than one result",
        })}
        description={intl.formatMessage({
          id: "config.tasks.identify.skip_multiple_matches_tooltip",
          defaultMessage:
            "If disabled, one result will be picked at random when multiple results are returned.",
        })}
        value={options.skipMultipleMatches}
        allowDefault={allowDefault}
        defaultValue={defaultOptions?.skipMultipleMatches}
        onChange={(v) => set({ skipMultipleMatches: v })}
      />
      {options.skipMultipleMatches && (
        <div className="ml-6 space-y-1">
          <Label className="text-xs">
            {intl.formatMessage({
              id: "config.tasks.identify.tag_skipped_matches",
              defaultMessage: "Tag skipped matches with",
            })}
          </Label>
          <TagIdPicker
            value={options.skipMultipleMatchTag}
            onChange={(id) => set({ skipMultipleMatchTag: id ?? undefined })}
            placeholder="Select tag…"
          />
        </div>
      )}

      <TriBoolean
        id="skip-single-name-performers"
        label={intl.formatMessage({
          id: "config.tasks.identify.skip_single_name_performers",
          defaultMessage: "Skip single name performers with no disambiguation",
        })}
        description={intl.formatMessage({
          id: "config.tasks.identify.skip_single_name_performers_tooltip",
          defaultMessage:
            "Skip performers with generic single names like 'Samantha' or 'Olga'.",
        })}
        value={options.skipSingleNamePerformers}
        allowDefault={allowDefault}
        defaultValue={defaultOptions?.skipSingleNamePerformers}
        onChange={(v) => set({ skipSingleNamePerformers: v })}
      />
      {options.skipSingleNamePerformers && (
        <div className="ml-6 space-y-1">
          <Label className="text-xs">
            {intl.formatMessage({
              id: "config.tasks.identify.tag_skipped_performers",
              defaultMessage: "Tag skipped performers with",
            })}
          </Label>
          <TagIdPicker
            value={options.skipSingleNamePerformerTag}
            onChange={(id) =>
              set({ skipSingleNamePerformerTag: id ?? undefined })
            }
            placeholder="Select tag…"
          />
        </div>
      )}

      <div className="space-y-2">
        <Label>
          {intl.formatMessage({
            id: "config.tasks.identify.field_options",
            defaultMessage: "Field options",
          })}
        </Label>
        <IdentifyFieldOptionsTable
          fieldOptions={options.fieldOptions ?? []}
          setFieldOptions={(f) => set({ fieldOptions: f })}
          allowSetDefault={allowDefault}
          defaultOptions={defaultOptions}
        />
      </div>
    </div>
  );
}
