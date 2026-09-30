import { useId, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import {
  PluginPreviewEntityV3,
  PluginSettingEditorV3,
} from "@/core/generated-graphql";
import { useMsg } from "@/hooks/message";
import {
  createPluginExpressionsAPI,
  createPluginSettingsAPI,
} from "@/plugins/settings-api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { useMappingValue } from "./plugin-mapping-editor";
import {
  PluginPreviewEntityPicker,
  type PreviewEntityOption,
} from "./plugin-preview-entity-picker";
import type { Setting } from "./plugin-settings-form";

export function PluginExpressionPreview({
  pluginId,
  setting,
  expression,
}: {
  pluginId: string;
  setting: Setting;
  expression: unknown;
}) {
  const msg = useMsg();
  const id = useId();
  const apollo = useApolloClient();
  const mappingValue = useMappingValue();
  const form = useForm({
    defaultValues: { input: "{}", entity: null as PreviewEntityOption | null },
  });
  const [output, setOutput] = useState<{
    input: string;
    expression: string;
    text: string;
  }>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState<"load" | "evaluate">();
  const expressionKey = JSON.stringify(expression);
  const preview = setting.preview;

  async function load() {
    const entity = form.state.values.entity;
    if (!entity) return;
    setPending("load");
    setError(undefined);
    setOutput(undefined);
    // Clear the previous entity's context even if reloading fails.
    form.setFieldValue("input", "{}");
    try {
      const context = await createPluginSettingsAPI(apollo, pluginId).preview(
        setting.name,
        entity.id,
      );
      form.setFieldValue("input", JSON.stringify(context, null, 2));
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    } finally {
      setPending(undefined);
    }
  }

  async function evaluate() {
    const input = form.state.values.input;
    setPending("evaluate");
    setError(undefined);
    setOutput(undefined);
    try {
      const data: unknown = JSON.parse(input);
      const api = createPluginExpressionsAPI(apollo);
      const result =
        setting.editor === PluginSettingEditorV3.JqMap
          ? await api.map(
              mappingValue(expression, setting.mapping_targets),
              data,
            )
          : await api.jq(
              typeof expression === "string" ? expression : "",
              data,
            );
      setOutput({
        input,
        expression: expressionKey,
        text: JSON.stringify(result, null, 2),
      });
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    } finally {
      setPending(undefined);
    }
  }

  return (
    <FieldGroup
      className="gap-3"
      onKeyDown={(event) => {
        // The preview is inside the settings form. Enter in its search input
        // must never submit that form and persist an unrelated draft.
        if (event.key === "Enter" && event.target instanceof HTMLInputElement) {
          event.preventDefault();
        }
      }}
    >
      <FieldDescription>
        {msg(
          "config.plugins.preview_readonly",
          "Test the current draft without saving settings or changing data. The result contains only the expression output.",
        )}
      </FieldDescription>
      {preview && (
        <form.Field name="entity">
          {(field) => (
            <Field>
              <FieldLabel htmlFor={`${id}-entity`}>
                {preview.entity === PluginPreviewEntityV3.Scene
                  ? msg("config.plugins.preview_scene", "Scene to preview")
                  : msg("config.plugins.preview_image", "Image to preview")}
              </FieldLabel>
              <PluginPreviewEntityPicker
                id={`${id}-entity`}
                entity={preview.entity}
                value={field.state.value}
                onChange={(value) => {
                  field.handleChange(value);
                  form.setFieldValue("input", "{}");
                  setOutput(undefined);
                  setError(undefined);
                }}
                disabled={!!pending}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!!pending || !field.state.value}
                onClick={() => void load()}
              >
                {pending === "load" && <Spinner data-icon="inline-start" />}
                {msg("config.plugins.load_preview_entity", "Load entity data")}
              </Button>
              {preview.description && (
                <FieldDescription>{preview.description}</FieldDescription>
              )}
            </Field>
          )}
        </form.Field>
      )}
      <form.Field name="input" validators={{ onChange: z.string() }}>
        {(field) => (
          <>
            <Field data-invalid={!!error}>
              <FieldLabel htmlFor={id}>
                {msg("config.plugins.sample_input", "Sample input (JSON)")}
              </FieldLabel>
              <Textarea
                id={id}
                value={field.state.value}
                rows={8}
                spellCheck={false}
                className="max-h-80 overflow-auto font-mono text-xs"
                disabled={!!pending}
                onChange={(event) => {
                  field.handleChange(event.target.value);
                  setError(undefined);
                }}
                aria-invalid={!!error}
                aria-describedby={error ? `${id}-error` : undefined}
              />
              {error && (
                <FieldError id={`${id}-error`} role="alert">
                  {error}
                </FieldError>
              )}
            </Field>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!!pending}
              onClick={() => void evaluate()}
            >
              {pending === "evaluate" && <Spinner data-icon="inline-start" />}
              {msg("config.plugins.test_expression", "Test expression")}
            </Button>
            {output &&
              output.input === field.state.value &&
              output.expression === expressionKey && (
                <pre
                  className="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs"
                  data-selectable-text
                  aria-live="polite"
                >
                  {output.text}
                </pre>
              )}
          </>
        )}
      </form.Field>
    </FieldGroup>
  );
}
