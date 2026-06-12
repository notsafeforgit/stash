import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery } from "@apollo/client/react";
import {
  ChevronDown,
  ChevronRight,
  ExternalLink,
  RefreshCw,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigurePlugin } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import { Switch } from "src/components/ui/switch";
import {
  SettingNumber,
  SettingsSection,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";
import { PackageManager } from "src/components/settings/package-manager";
import { PluginHookOrder } from "src/components/settings/plugin-hook-order";

type Plugin = NonNullable<GQL.PluginsQuery["plugins"]>[number];

function PluginSettingRow({
  setting,
  value,
  onChange,
}: {
  setting: NonNullable<Plugin["settings"]>[number];
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const label = setting.display_name || setting.name;
  switch (setting.type) {
    case GQL.PluginSettingTypeEnum.Boolean:
      return (
        <SettingSwitch
          label={label}
          description={setting.description}
          checked={value === true}
          onChange={(v) => onChange(v)}
        />
      );
    case GQL.PluginSettingTypeEnum.Number:
      return (
        <SettingNumber
          label={label}
          description={setting.description}
          value={typeof value === "number" ? value : 0}
          onChange={(v) => onChange(v)}
        />
      );
    case GQL.PluginSettingTypeEnum.String:
      return (
        <SettingText
          label={label}
          description={setting.description}
          value={typeof value === "string" ? value : ""}
          onChange={(v) => onChange(v)}
        />
      );
    default:
      return null;
  }
}

function PluginCard({ plugin }: { plugin: Plugin }) {
  const Toast = useToast();
  const { configuration } = useConfigurationContext();
  const [configurePlugin] = useConfigurePlugin();
  const [setPluginsEnabled] = useMutation(GQL.SetPluginsEnabledDocument, {
    refetchQueries: [{ query: GQL.PluginsDocument }],
  });

  const [expanded, setExpanded] = useState(false);
  const [needsReload, setNeedsReload] = useState(false);

  const pluginsConfig = (configuration.plugins ?? {}) as Record<
    string,
    Record<string, unknown>
  >;
  const pluginSettings = pluginsConfig[plugin.id] ?? {};

  const msg = useMsg();

  async function onToggleEnabled() {
    try {
      await setPluginsEnabled({
        variables: { enabledMap: { [plugin.id]: !plugin.enabled } },
      });
      setNeedsReload(true);
    } catch (e) {
      Toast.error(e);
    }
  }

  const hasDetails =
    (plugin.hooks?.length ?? 0) > 0 || (plugin.settings?.length ?? 0) > 0;

  return (
    <div className={cn("rounded-lg border", !plugin.enabled && "opacity-60")}>
      <div className="flex flex-col gap-2 p-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className={cn(
                "h-auto gap-1 p-0 text-sm font-medium hover:bg-transparent",
                hasDetails && "cursor-pointer hover:text-primary",
              )}
              onClick={() => hasDetails && setExpanded((v) => !v)}
            >
              {hasDetails &&
                (expanded ? (
                  <ChevronDown className="size-4" />
                ) : (
                  <ChevronRight className="size-4" />
                ))}
              {plugin.name}
              {plugin.version && (
                <span className="font-normal text-muted-foreground">
                  ({plugin.version})
                </span>
              )}
            </Button>
            {plugin.url && (
              <a
                href={plugin.url}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`${plugin.name} homepage`}
                className="text-muted-foreground hover:text-foreground"
              >
                <ExternalLink className="size-4" />
              </a>
            )}
          </div>
          {plugin.description && (
            <p className="text-sm text-muted-foreground">
              {plugin.description}
            </p>
          )}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {needsReload && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => window.location.reload()}
            >
              {msg("actions.reload_ui", "Reload UI")}
            </Button>
          )}
          <Switch
            checked={plugin.enabled}
            onCheckedChange={() => void onToggleEnabled()}
            aria-label={
              plugin.enabled
                ? msg("actions.disable", "Disable")
                : msg("actions.enable", "Enable")
            }
          />
        </div>
      </div>

      {expanded && hasDetails && (
        <div className="space-y-4 border-t p-3">
          {!!plugin.hooks?.length && (
            <div className="space-y-2">
              <h4 className="text-sm font-medium">
                {msg("config.plugins.hooks", "Hooks")}
              </h4>
              {plugin.hooks.map((h) => (
                <div key={h.name} className="text-sm">
                  <div className="font-medium">{h.name}</div>
                  {h.description && (
                    <div className="text-muted-foreground">{h.description}</div>
                  )}
                  {!!h.hooks?.length && (
                    <div className="mt-1 flex flex-wrap gap-1">
                      {h.hooks.map((hh) => (
                        <code
                          key={hh}
                          className="rounded bg-muted px-1.5 py-0.5 text-xs"
                        >
                          {hh}
                        </code>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
          {!!plugin.settings?.length && (
            <div className="space-y-3">
              <h4 className="text-sm font-medium">
                {msg("settings", "Settings")}
              </h4>
              {plugin.settings.map((setting) => (
                <PluginSettingRow
                  key={setting.name}
                  setting={setting}
                  value={pluginSettings[setting.name]}
                  onChange={(v) =>
                    void configurePlugin({
                      variables: {
                        plugin_id: plugin.id,
                        input: { ...pluginSettings, [setting.name]: v },
                      },
                    })
                  }
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function SettingsPluginsPage() {
  const Toast = useToast();
  const { data, loading, refetch } = useQuery(GQL.PluginsDocument);
  const [reloadPlugins] = useMutation(GQL.ReloadPluginsDocument, {
    refetchQueries: [{ query: GQL.PluginsDocument }],
  });

  const msg = useMsg();

  async function onReloadPlugins() {
    try {
      await reloadPlugins();
      Toast.success(msg("toast.reloaded_plugins", "Reloaded plugins"));
    } catch (e) {
      Toast.error(e);
    }
  }

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection
        title={msg("config.plugins.plugin_packages", "Plugin packages")}
      >
        <PackageManager
          type="plugin"
          onPackagesChanged={() => void refetch()}
        />
      </SettingsSection>

      <SettingsSection title={msg("config.categories.plugins", "Plugins")}>
        <div>
          <Button
            type="button"
            variant="outline"
            onClick={() => void onReloadPlugins()}
          >
            <RefreshCw className="size-4" />
            {msg("actions.reload_plugins", "Reload plugins")}
          </Button>
        </div>
        {loading ? (
          <Spinner className="size-5" />
        ) : (
          <div className="space-y-3">
            {(data?.plugins ?? []).map((plugin) => (
              <PluginCard key={plugin.id} plugin={plugin} />
            ))}
            {!data?.plugins?.length && (
              <p className="text-sm text-muted-foreground">
                {msg("config.plugins.no_plugins", "No plugins installed.")}
              </p>
            )}
          </div>
        )}
      </SettingsSection>

      <SettingsSection
        title={msg("plugin_hook_order.heading", "Plugin hook order")}
      >
        <PluginHookOrder />
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/plugins")({
  component: SettingsPluginsPage,
});
