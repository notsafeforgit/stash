// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { IntlProvider } from "react-intl";
import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { ApolloProvider } from "@apollo/client/react";
import { expect, it, vi } from "vitest";
import {
  PluginSettingEditorV3,
  PluginSettingTypeV3,
} from "src/core/generated-graphql";
import { PluginSettingsForm } from "./plugin-settings-form";

it.each([PluginSettingTypeV3.String, PluginSettingTypeV3.Json])(
  "keeps invalid %s mapping drafts, retries saves, and patches only edited settings",
  async (mappingType) => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    const requests: Record<string, unknown>[] = [];
    const apollo = new ApolloClient({
      cache: new InMemoryCache(),
      link: new ApolloLink(
        (operation) =>
          new Observable((observer) => {
            requests.push(operation.variables);
            const input = operation.variables.input as Record<string, unknown>;
            if (input.mappings === "invalid")
              observer.error(new Error("Expected a JSON object"));
            else {
              observer.next({
                data: {
                  updatePluginSettings: {
                    other: "external",
                    enabled: false,
                    ...input,
                  },
                },
              });
              observer.complete();
            }
          }),
      ),
    });
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);
    const settings: Parameters<typeof PluginSettingsForm>[0]["settings"] = [
      {
        __typename: "PluginSettingV3",
        name: "mappings",
        display_name: null,
        description: null,
        type: mappingType,
        editor: PluginSettingEditorV3.JqMap,
        default_value: mappingType === PluginSettingTypeV3.Json ? {} : "{}",
        options: [],
      },
      {
        __typename: "PluginSettingV3",
        name: "other",
        display_name: null,
        description: null,
        editor: null,
        type: PluginSettingTypeV3.String,
        default_value: "original",
        options: [],
      },
      {
        __typename: "PluginSettingV3",
        name: "enabled",
        display_name: null,
        description: null,
        editor: null,
        type: PluginSettingTypeV3.Boolean,
        default_value: false,
        options: [],
      },
    ];
    const render = (saved: Record<string, unknown>) =>
      root.render(
        <ApolloProvider client={apollo}>
          <IntlProvider locale="en">
            <PluginSettingsForm
              pluginId="fixture"
              settings={settings}
              saved={saved}
            />
          </IntlProvider>
        </ApolloProvider>,
      );
    const setText = async (text: string) => {
      const textarea = container.querySelector("textarea");
      if (!textarea) throw new Error("Missing mapping editor");
      await act(async () => {
        Object.getOwnPropertyDescriptor(
          HTMLTextAreaElement.prototype,
          "value",
        )?.set?.call(textarea, text);
        textarea.dispatchEvent(new Event("input", { bubbles: true }));
      });
    };
    const submit = () =>
      act(async () => {
        container
          .querySelector("form")
          ?.dispatchEvent(
            new Event("submit", { bubbles: true, cancelable: true }),
          );
      });
    try {
      await act(async () => render({}));
      expect(container.querySelector("textarea")?.value).toBe("{}");
      expect(
        container
          .querySelector('[role="switch"]')
          ?.getAttribute("aria-checked"),
      ).toBe("false");
      await setText("invalid");
      await submit();
      expect(container.querySelector('[role="alert"]')).not.toBeNull();
      expect(container.querySelector("textarea")?.value).toBe("invalid");
      await act(async () => render({ other: "external" }));
      await setText('{"title":".catalog.title"}');
      await submit();
      expect(requests.at(-1)).toEqual({
        plugin_id: "fixture",
        input: {
          mappings:
            mappingType === PluginSettingTypeV3.Json
              ? { title: ".catalog.title" }
              : '{"title":".catalog.title"}',
        },
        reset: [],
      });
      expect(container.querySelector('[role="alert"]')).toBeNull();
      expect(
        container.querySelector<HTMLInputElement>(
          'input[type="text"], input:not([type])',
        )?.value,
      ).toBe("external");
    } finally {
      await act(async () => root.unmount());
      apollo.stop();
      container.remove();
      vi.unstubAllGlobals();
    }
  },
);
