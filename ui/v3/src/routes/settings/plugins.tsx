import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useMutation, useQuery } from "@apollo/client/react";
import { ChevronRight, ExternalLink, RefreshCw } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { cn } from "src/lib/utils";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import { Switch } from "src/components/ui/switch";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { SettingsSection } from "src/components/settings/setting-row";
import { PluginSettingsForm } from "src/components/settings/plugin-settings-form";
import { PackageManager } from "src/components/settings/package-manager";
import { PluginHookOrder } from "src/components/settings/plugin-hook-order";
import { PluginPageLinks, ReloadPluginPages } from "src/plugins/page-links";

type Plugin = NonNullable<GQL.PluginsQuery["plugins"]>[number];

function PluginCard({ plugin }: { plugin: Plugin }) {
  const Toast = useToast();
  const { configuration } = useConfigurationContext();
  const [setPluginsEnabled] = useMutation(GQL.SetPluginsEnabledDocument, {
    refetchQueries: [{ query: GQL.PluginsDocument }],
  });

  const [detailsReady, setDetailsReady] = useState(false);
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
    <Collapsible
      onOpenChange={(open) => {
        if (open) setDetailsReady(true);
      }}
      className={cn("rounded-lg border", !plugin.enabled && "opacity-60")}
    >
      <div className="flex flex-col gap-2 p-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <CollapsibleTrigger
              disabled={!hasDetails}
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="group"
                />
              }
            >
              {hasDetails && (
                <ChevronRight
                  data-icon="inline-start"
                  className="transition-transform group-aria-expanded:rotate-90"
                />
              )}
              {plugin.name}
              {plugin.version && (
                <span className="font-normal text-muted-foreground">
                  ({plugin.version})
                </span>
              )}
            </CollapsibleTrigger>
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
          {plugin.enabled && !needsReload && (
            <PluginPageLinks pluginId={plugin.id} />
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

      <CollapsibleContent keepMounted>
        {detailsReady && hasDetails && (
          <div className="flex flex-col gap-4 border-t p-3">
            {!!plugin.hooks?.length && (
              <div className="flex flex-col gap-2">
                <h4 className="text-sm font-medium">
                  {msg("config.plugins.hooks", "Hooks")}
                </h4>
                {plugin.hooks.map((h) => (
                  <div key={h.name} className="text-sm">
                    <div className="font-medium">{h.name}</div>
                    {h.description && (
                      <div className="text-muted-foreground">
                        {h.description}
                      </div>
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
              <div className="flex flex-col gap-3">
                <h4 className="text-sm font-medium">
                  {msg("settings", "Settings")}
                </h4>
                <PluginSettingsForm
                  pluginId={plugin.id}
                  settings={plugin.settings}
                  saved={pluginSettings}
                />
              </div>
            )}
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

function SettingsPluginsPage() {
  const Toast = useToast();
  const [packagesChanged, setPackagesChanged] = useState(false);
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
          onPackagesChanged={() => {
            setPackagesChanged(true);
            void refetch();
          }}
        />
        {packagesChanged && <ReloadPluginPages />}
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
