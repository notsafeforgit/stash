import { PluginExpressionPreview } from "./plugin-expression-preview";
import { useId, useState, type ReactNode } from "react";
import { useApolloClient } from "@apollo/client/react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import * as GQL from "src/core/generated-graphql";
import { useMsg } from "src/hooks/message";
import { useSaveIndicator } from "src/hooks/save-indicator";
import { createPluginSettingsAPI } from "src/plugins/settings-api";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Textarea } from "src/components/ui/textarea";
import { Switch } from "src/components/ui/switch";
import { Spinner } from "src/components/ui/spinner";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "src/components/ui/field";
import {
  mappingDraft,
  PluginMappingEditor,
  useMappingValue,
} from "./plugin-mapping-editor";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";

export type Setting = NonNullable<
  NonNullable<GQL.PluginsQuery["plugins"]>[number]["settings"]
>[number];

export function settingDefault(setting: Setting): unknown {
  if (setting.default_value != null) return setting.default_value;
  if (setting.type === GQL.PluginSettingTypeV3.Boolean) return false;
  if (setting.type === GQL.PluginSettingTypeV3.Number) return 0;
  if (setting.type === GQL.PluginSettingTypeV3.Json) {
    return setting.editor === GQL.PluginSettingEditorV3.JqMap ? {} : null;
  }
  return "";
}

function settingDraft(setting: Setting, value: unknown): unknown {
  if (setting.editor === GQL.PluginSettingEditorV3.JqMap) {
    return mappingDraft(value);
  }
  if (setting.type !== GQL.PluginSettingTypeV3.Json) return value;
  return JSON.stringify(value, null, 2);
}

function SettingControl({
  pluginId,
  setting,
  value,
  onChange,
  disabled,
}: {
  pluginId: string;
  setting: Setting;
  value: unknown;
  onChange: (value: unknown) => void;
  disabled: boolean;
}) {
  const id = useId();
  const msg = useMsg();
  const [preview, setPreview] = useState(false);
  const label = setting.display_name || setting.name;
  const editor = setting.editor;
  const text = typeof value === "string" ? value : "";
  const descriptionId = setting.description ? `${id}-description` : undefined;
  const previewControl = (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setPreview(!preview)}
        aria-expanded={preview}
      >
        {msg("config.plugins.preview_expression", "Preview mappings")}
      </Button>
      {preview && (
        <PluginExpressionPreview
          pluginId={pluginId}
          setting={setting}
          expression={value}
        />
      )}
    </>
  );
  if (editor === GQL.PluginSettingEditorV3.JqMap) {
    return (
      <FieldSet disabled={disabled} aria-describedby={descriptionId}>
        <FieldLegend variant="label">{label}</FieldLegend>
        {setting.description && (
          <FieldDescription id={descriptionId}>
            {setting.description}
          </FieldDescription>
        )}
        <PluginMappingEditor
          value={value}
          onChange={onChange}
          disabled={disabled}
        />
        {previewControl}
      </FieldSet>
    );
  }
  let control: ReactNode;
  if (setting.type === GQL.PluginSettingTypeV3.Boolean) {
    control = (
      <Switch
        id={id}
        checked={value === true}
        onCheckedChange={onChange}
        disabled={disabled}
        aria-describedby={descriptionId}
      />
    );
  } else if (setting.type === GQL.PluginSettingTypeV3.Number) {
    control = (
      <Input
        id={id}
        type="number"
        step="any"
        value={typeof value === "number" ? value : ""}
        onChange={(event) =>
          onChange(
            event.target.value === "" ? null : Number(event.target.value),
          )
        }
        disabled={disabled}
        aria-describedby={descriptionId}
      />
    );
  } else if (editor === GQL.PluginSettingEditorV3.Select) {
    control = (
      <Select
        value={text}
        onValueChange={(value) => {
          if (value !== null) onChange(value);
        }}
        disabled={disabled}
      >
        <SelectTrigger id={id} aria-describedby={descriptionId}>
          <SelectValue>
            {setting.options.find((option) => option.value === text)?.label ??
              text}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {setting.options.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    );
  } else if (editor && editor !== GQL.PluginSettingEditorV3.Text) {
    control = (
      <Textarea
        id={id}
        value={text}
        rows={4}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        aria-describedby={descriptionId}
        spellCheck={editor === GQL.PluginSettingEditorV3.Textarea}
      />
    );
  } else {
    control = (
      <Input
        id={id}
        value={text}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        aria-describedby={descriptionId}
      />
    );
  }
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {setting.description && (
        <FieldDescription id={descriptionId}>
          {setting.description}
        </FieldDescription>
      )}
      {control}
      {editor === GQL.PluginSettingEditorV3.Jq && previewControl}
    </Field>
  );
}

export function PluginSettingsForm({
  pluginId,
  settings,
  saved,
}: {
  pluginId: string;
  settings: Setting[];
  saved: Record<string, unknown>;
}) {
  const msg = useMsg();
  const apollo = useApolloClient();
  const mappingValue = useMappingValue();
  const { track } = useSaveIndicator();
  const [error, setError] = useState<string>();
  const values = Object.fromEntries(
    settings.map((setting) => [
      setting.name,
      settingDraft(
        setting,
        Object.hasOwn(saved, setting.name)
          ? saved[setting.name]
          : settingDefault(setting),
      ),
    ]),
  );
  // Diff against the values this draft started with, so saving one field cannot
  // overwrite another client's edits to an untouched field.
  const [baseline, setBaseline] = useState(values);
  const form = useForm({
    defaultValues: { values: baseline },
    onSubmit: async ({ value }) => {
      setError(undefined);
      try {
        const patch = Object.fromEntries(
          settings
            .filter(
              (setting) =>
                JSON.stringify(value.values[setting.name]) !==
                JSON.stringify(baseline[setting.name]),
            )
            .map((setting) => {
              const draft = value.values[setting.name];
              if (setting.editor === GQL.PluginSettingEditorV3.JqMap) {
                const mappings = mappingValue(draft);
                return [
                  setting.name,
                  setting.type === GQL.PluginSettingTypeV3.Json
                    ? mappings
                    : JSON.stringify(mappings),
                ];
              }
              return [
                setting.name,
                setting.type === GQL.PluginSettingTypeV3.Json
                  ? JSON.parse(String(draft))
                  : draft,
              ];
            }),
        );
        const updated = await track(
          createPluginSettingsAPI(apollo, pluginId).update(patch),
        );
        const next = Object.fromEntries(
          settings.map((setting) => [
            setting.name,
            settingDraft(
              setting,
              Object.hasOwn(updated, setting.name)
                ? updated[setting.name]
                : settingDefault(setting),
            ),
          ]),
        );
        setBaseline(next);
        form.reset({ values: next });
      } catch (error) {
        setError(error instanceof Error ? error.message : String(error));
      }
    },
  });

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <form.Subscribe selector={(state) => state.isSubmitting}>
        {(pending) => (
          <form.Field
            name="values"
            validators={{ onSubmit: z.record(z.string(), z.unknown()) }}
          >
            {(field) => (
              <FieldGroup>
                {settings.map((setting) => (
                  <SettingControl
                    key={setting.name}
                    pluginId={pluginId}
                    setting={setting}
                    value={field.state.value[setting.name]}
                    onChange={(value) =>
                      field.handleChange({
                        ...field.state.value,
                        [setting.name]: value,
                      })
                    }
                    disabled={pending}
                  />
                ))}
                {error && <FieldError role="alert">{error}</FieldError>}
                <div className="flex gap-2">
                  <Button type="submit" disabled={pending}>
                    {pending && <Spinner data-icon="inline-start" />}
                    {msg("actions.save", "Save")}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={pending}
                    onClick={() => {
                      setBaseline(values);
                      form.reset({ values });
                      setError(undefined);
                    }}
                  >
                    {msg("actions.cancel", "Cancel")}
                  </Button>
                </div>
              </FieldGroup>
            )}
          </form.Field>
        )}
      </form.Subscribe>
    </form>
  );
}
