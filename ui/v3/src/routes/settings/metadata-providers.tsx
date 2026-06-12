import { useMemo, useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useApolloClient, useMutation, useQuery } from "@apollo/client/react";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import {
  useConfigurationContext,
  useConfigureGeneral,
  useConfigureScraping,
} from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Spinner } from "src/components/ui/spinner";
import {
  SettingPath,
  SettingsSection,
  SettingStringList,
  SettingSwitch,
  SettingText,
} from "src/components/settings/setting-row";
import { StashBoxSettings } from "src/components/settings/stash-box-settings";
import { PackageManager } from "src/components/settings/package-manager";

interface ScraperRow {
  id: string;
  name: string;
  types: GQL.ScrapeType[];
  urls: string[];
}

function ScraperTable({
  entityLabel,
  scrapers,
}: {
  entityLabel: string;
  scrapers: ScraperRow[];
}) {
  const intl = useIntl();
  const [open, setOpen] = useState(false);

  function typeLabel(t: GQL.ScrapeType) {
    if (t === GQL.ScrapeType.Fragment) {
      return intl.formatMessage(
        {
          id: "config.scraping.entity_metadata",
          defaultMessage: "{entityType} metadata",
        },
        { entityType: entityLabel },
      );
    }
    return t;
  }

  return (
    <div className="rounded-lg border">
      <Button
        type="button"
        variant="ghost"
        className="w-full justify-start gap-2 px-3"
        onClick={() => setOpen((v) => !v)}
      >
        {open ? (
          <ChevronDown className="size-4" />
        ) : (
          <ChevronRight className="size-4" />
        )}
        <span>
          {intl.formatMessage(
            {
              id: "config.scraping.entity_scrapers",
              defaultMessage: "{entityType} scrapers",
            },
            { entityType: entityLabel },
          )}
        </span>
        <span className="text-muted-foreground">({scrapers.length})</span>
      </Button>
      {open && (
        <div className="overflow-x-auto border-t">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b bg-muted/50 text-left">
                <th className="p-2 font-medium">
                  {intl.formatMessage({ id: "name", defaultMessage: "Name" })}
                </th>
                <th className="p-2 font-medium">
                  {intl.formatMessage({
                    id: "config.scraping.supported_types",
                    defaultMessage: "Supported types",
                  })}
                </th>
                <th className="p-2 font-medium">
                  {intl.formatMessage({
                    id: "config.scraping.supported_urls",
                    defaultMessage: "URLs",
                  })}
                </th>
              </tr>
            </thead>
            <tbody>
              {scrapers.map((s) => (
                <tr key={s.id} className="border-b align-top last:border-b-0">
                  <td className="p-2">{s.name}</td>
                  <td className="p-2">
                    <ul>
                      {s.types.map((t) => (
                        <li key={t}>{typeLabel(t)}</li>
                      ))}
                    </ul>
                  </td>
                  <td className="max-w-72 p-2">
                    <ul className="space-y-0.5">
                      {s.urls.slice(0, 8).map((u) => (
                        <li key={u} className="truncate text-xs">
                          {u}
                        </li>
                      ))}
                      {s.urls.length > 8 && (
                        <li className="text-xs text-muted-foreground">
                          +{s.urls.length - 8} more
                        </li>
                      )}
                    </ul>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function matchesFilter(filter: string, name: string, urls: string[]) {
  if (!filter) return true;
  return (
    name.toLowerCase().includes(filter) ||
    urls.some((url) => url.toLowerCase().includes(filter))
  );
}

function ScrapersSection() {
  const intl = useIntl();
  const Toast = useToast();
  const [filter, setFilter] = useState("");
  const [reloadScrapers] = useMutation(GQL.ReloadScrapersDocument);

  const { data: sceneData, loading: l1 } = useQuery(
    GQL.ListSceneScrapersDocument,
  );
  const { data: galleryData, loading: l2 } = useQuery(
    GQL.ListGalleryScrapersDocument,
  );
  const { data: imageData, loading: l3 } = useQuery(
    GQL.ListImageScrapersDocument,
  );
  const { data: performerData, loading: l4 } = useQuery(
    GQL.ListPerformerScrapersDocument,
  );
  const { data: groupData, loading: l5 } = useQuery(
    GQL.ListGroupScrapersDocument,
  );

  const loading = l1 || l2 || l3 || l4 || l5;

  const tables = useMemo(() => {
    const f = filter.toLowerCase();

    function rows<T>(
      scrapers: T[] | undefined,
      pick: (s: T) => {
        id: string;
        name: string;
        spec?: {
          supported_scrapes: GQL.ScrapeType[];
          urls?: string[] | null;
        } | null;
      },
    ): ScraperRow[] {
      return (scrapers ?? [])
        .map(pick)
        .filter((s) => s.spec)
        .filter((s) => matchesFilter(f, s.name, s.spec?.urls ?? []))
        .map((s) => ({
          id: s.id,
          name: s.name,
          types: s.spec?.supported_scrapes ?? [],
          urls: s.spec?.urls ?? [],
        }));
    }

    return [
      {
        entity: intl.formatMessage({ id: "scene", defaultMessage: "Scene" }),
        rows: rows(sceneData?.listScrapers, (s) => ({
          id: s.id,
          name: s.name,
          spec: s.scene,
        })),
      },
      {
        entity: intl.formatMessage({
          id: "gallery",
          defaultMessage: "Gallery",
        }),
        rows: rows(galleryData?.listScrapers, (s) => ({
          id: s.id,
          name: s.name,
          spec: s.gallery,
        })),
      },
      {
        entity: intl.formatMessage({ id: "image", defaultMessage: "Image" }),
        rows: rows(imageData?.listScrapers, (s) => ({
          id: s.id,
          name: s.name,
          spec: s.image,
        })),
      },
      {
        entity: intl.formatMessage({
          id: "performer",
          defaultMessage: "Performer",
        }),
        rows: rows(performerData?.listScrapers, (s) => ({
          id: s.id,
          name: s.name,
          spec: s.performer,
        })),
      },
      {
        entity: intl.formatMessage({ id: "group", defaultMessage: "Group" }),
        rows: rows(groupData?.listScrapers, (s) => ({
          id: s.id,
          name: s.name,
          spec: s.group,
        })),
      },
    ].filter((t) => t.rows.length > 0);
  }, [
    filter,
    intl,
    sceneData,
    galleryData,
    imageData,
    performerData,
    groupData,
  ]);

  async function onReloadScrapers() {
    try {
      await reloadScrapers();
      Toast.success(
        intl.formatMessage({
          id: "toast.reloaded_scrapers",
          defaultMessage: "Reloaded scrapers",
        }),
      );
    } catch (e) {
      Toast.error(e);
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          className="w-56"
          placeholder={`${intl.formatMessage({ id: "filter", defaultMessage: "Filter" })}…`}
          value={filter}
          onChange={(e) => setFilter(e.currentTarget.value)}
        />
        <Button
          type="button"
          variant="outline"
          onClick={() => void onReloadScrapers()}
        >
          <RefreshCw className="size-4" />
          {intl.formatMessage({
            id: "actions.reload_scrapers",
            defaultMessage: "Reload scrapers",
          })}
        </Button>
      </div>
      {loading ? (
        <Spinner className="size-5" />
      ) : (
        <div className="space-y-2">
          {tables.map((t) => (
            <ScraperTable
              key={t.entity}
              entityLabel={t.entity}
              scrapers={t.rows}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function SettingsMetadataProvidersPage() {
  const apolloClient = useApolloClient();
  const { configuration } = useConfigurationContext();
  const general = configuration.general;
  const scraping = configuration.scraping;
  const [configureGeneral] = useConfigureGeneral();
  const [configureScraping] = useConfigureScraping();

  const msg = useMsg();

  function saveScraping(input: Partial<GQL.ConfigScrapingInput>) {
    void configureScraping({ variables: { input } });
  }

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection
        title={msg("config.stashbox.title", "Stash-box endpoints")}
        description={msg(
          "config.stashbox.description",
          "Stash-box facilitates automated tagging of scenes and performers based on fingerprints and filenames.",
        )}
      >
        <StashBoxSettings
          boxes={general.stashBoxes.map((b) => ({
            name: b.name,
            endpoint: b.endpoint,
            api_key: b.api_key,
            max_requests_per_minute: b.max_requests_per_minute,
          }))}
          onChange={(boxes) =>
            void configureGeneral({
              variables: { input: { stashBoxes: boxes } },
            })
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.general.scraping", "Scraping")}>
        <SettingText
          label={msg("config.general.scraper_user_agent", "Scraper user agent")}
          description={msg(
            "config.general.scraper_user_agent_desc",
            "User-Agent string used during scrape HTTP requests",
          )}
          value={scraping.scraperUserAgent ?? ""}
          onChange={(v) => saveScraping({ scraperUserAgent: v })}
        />
        <SettingPath
          label={msg("config.general.chrome_cdp_path", "Chrome CDP path")}
          description={msg(
            "config.general.chrome_cdp_path_desc",
            "File path to the Chrome executable, or a remote address (starting with http:// or https://, for example http://localhost:9222/json/version) of a Chrome instance.",
          )}
          value={scraping.scraperCDPPath ?? ""}
          onChange={(v) => saveScraping({ scraperCDPPath: v })}
          picker={false}
        />
        <SettingSwitch
          label={msg(
            "config.general.check_for_insecure_certificates",
            "Check for insecure certificates",
          )}
          description={msg(
            "config.general.check_for_insecure_certificates_desc",
            "Some sites use insecure SSL certificates. When unchecked the scraper skips the insecure certificates check.",
          )}
          checked={scraping.scraperCertCheck}
          onChange={(v) => saveScraping({ scraperCertCheck: v })}
        />
        <SettingStringList
          label={msg(
            "config.scraping.excluded_tag_patterns_head",
            "Excluded tag patterns",
          )}
          description={msg(
            "config.scraping.excluded_tag_patterns_desc",
            "Regexps of tag names to exclude from scrape results",
          )}
          value={scraping.excludeTagPatterns}
          onChange={(v) => saveScraping({ excludeTagPatterns: v })}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.scraping.scraper_packages", "Scraper packages")}
      >
        <PackageManager
          type="scraper"
          onPackagesChanged={() =>
            void apolloClient.refetchQueries({
              include: [
                GQL.ListSceneScrapersDocument,
                GQL.ListGalleryScrapersDocument,
                GQL.ListImageScrapersDocument,
                GQL.ListPerformerScrapersDocument,
                GQL.ListGroupScrapersDocument,
              ],
            })
          }
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.scraping.scrapers", "Installed scrapers")}
      >
        <ScrapersSection />
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/metadata-providers")({
  component: SettingsMetadataProvidersPage,
});
