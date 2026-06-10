import { useMemo } from "react";
import { useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";

export interface AvailableScraper {
  kind: "scraper";
  id: string;
  name: string;
  urls: string[];
  supports: GQL.ScrapeType[];
}

export interface AvailableStashBox {
  kind: "stashBox";
  endpoint: string;
  name: string;
}

export type ScrapeSource = AvailableScraper | AvailableStashBox;

export interface AvailableScrapeSources {
  scrapers: AvailableScraper[];
  stashBoxes: AvailableStashBox[];
  loading: boolean;
  hasAny: boolean;
}

/**
 * Lists scrapers exposing a `performer` spec plus any configured stash-boxes
 * (which always support NAME + FRAGMENT performer scrapes via the
 * `stash_box_endpoint` source). Stash-boxes are read from the global
 * configuration query.
 */
export function useAvailablePerformerScrapers(): AvailableScrapeSources {
  const scrapersQ = useQuery(GQL.ListPerformerScrapersDocument);
  const configQ = useQuery(GQL.ConfigurationDocument);

  const scrapers = useMemo<AvailableScraper[]>(() => {
    const list = scrapersQ.data?.listScrapers ?? [];
    return list
      .filter((s) => s.performer && s.performer.supported_scrapes.length > 0)
      .map((s) => ({
        kind: "scraper" as const,
        id: s.id,
        name: s.name,
        urls: s.performer?.urls ?? [],
        supports: s.performer?.supported_scrapes ?? [],
      }));
  }, [scrapersQ.data]);

  const stashBoxes = useMemo<AvailableStashBox[]>(() => {
    const list = configQ.data?.configuration.general.stashBoxes ?? [];
    return list.map((b) => ({
      kind: "stashBox" as const,
      endpoint: b.endpoint,
      // Fall back to host-portion of endpoint when the user hasn't set a name.
      name: b.name || b.endpoint,
    }));
  }, [configQ.data]);

  return {
    scrapers,
    stashBoxes,
    loading: scrapersQ.loading || configQ.loading,
    hasAny: scrapers.length + stashBoxes.length > 0,
  };
}

/**
 * Lists scrapers exposing a `scene` spec plus any configured stash-boxes.
 */
export function useAvailableSceneScrapers(): AvailableScrapeSources {
  const scrapersQ = useQuery(GQL.ListSceneScrapersDocument);
  const configQ = useQuery(GQL.ConfigurationDocument);

  const scrapers = useMemo<AvailableScraper[]>(() => {
    const list = scrapersQ.data?.listScrapers ?? [];
    return list
      .filter((s) => s.scene && s.scene.supported_scrapes.length > 0)
      .map((s) => ({
        kind: "scraper" as const,
        id: s.id,
        name: s.name,
        urls: s.scene?.urls ?? [],
        supports: s.scene?.supported_scrapes ?? [],
      }));
  }, [scrapersQ.data]);

  const stashBoxes = useMemo<AvailableStashBox[]>(() => {
    const list = configQ.data?.configuration.general.stashBoxes ?? [];
    return list.map((b) => ({
      kind: "stashBox" as const,
      endpoint: b.endpoint,
      name: b.name || b.endpoint,
    }));
  }, [configQ.data]);

  return {
    scrapers,
    stashBoxes,
    loading: scrapersQ.loading || configQ.loading,
    hasAny: scrapers.length + stashBoxes.length > 0,
  };
}

/**
 * Lists scrapers with a `studio` spec plus stash-boxes. Stash-boxes can
 * search studios via the same scrapeSingleStudio endpoint.
 */
export function useAvailableStudioScrapers(): AvailableScrapeSources {
  const scrapersQ = useQuery(GQL.ListSceneScrapersDocument);
  const configQ = useQuery(GQL.ConfigurationDocument);

  // Note: there's no dedicated ListStudioScrapers query; studio scraping is
  // typically attached to scene scrapers. We surface stash-boxes only here —
  // the `scrapers` list is left empty so the menu is stash-box-only.
  const scrapers = useMemo<AvailableScraper[]>(() => [], []);
  void scrapersQ;

  const stashBoxes = useMemo<AvailableStashBox[]>(() => {
    const list = configQ.data?.configuration.general.stashBoxes ?? [];
    return list.map((b) => ({
      kind: "stashBox" as const,
      endpoint: b.endpoint,
      name: b.name || b.endpoint,
    }));
  }, [configQ.data]);

  return {
    scrapers,
    stashBoxes,
    loading: configQ.loading,
    hasAny: scrapers.length + stashBoxes.length > 0,
  };
}

/**
 * Lists stash-boxes for tag stash-id search. There's no dedicated tag
 * scraper list query in v3; tag scraping is fed through scene scrapers.
 * Surfacing stash-boxes only matches v2.5's tag stash-id picker behaviour.
 */
export function useAvailableTagScrapers(): AvailableScrapeSources {
  const configQ = useQuery(GQL.ConfigurationDocument);

  const stashBoxes = useMemo<AvailableStashBox[]>(() => {
    const list = configQ.data?.configuration.general.stashBoxes ?? [];
    return list.map((b) => ({
      kind: "stashBox" as const,
      endpoint: b.endpoint,
      name: b.name || b.endpoint,
    }));
  }, [configQ.data]);

  return {
    scrapers: [],
    stashBoxes,
    loading: configQ.loading,
    hasAny: stashBoxes.length > 0,
  };
}

/**
 * Just the configured stash-boxes — for places that need only the box list
 * (e.g. the Stash IDs search button). Avoids firing the per-type scraper list
 * queries when the caller doesn't care about non-stash-box scrapers.
 */
export function useAvailableStashBoxes(): {
  stashBoxes: AvailableStashBox[];
  loading: boolean;
} {
  const configQ = useQuery(GQL.ConfigurationDocument);
  const stashBoxes = useMemo<AvailableStashBox[]>(() => {
    const list = configQ.data?.configuration.general.stashBoxes ?? [];
    return list.map((b) => ({
      kind: "stashBox" as const,
      endpoint: b.endpoint,
      name: b.name || b.endpoint,
    }));
  }, [configQ.data]);
  return { stashBoxes, loading: configQ.loading };
}

/** Build a ScraperSourceInput for the picked source. */
export function sourceToInput(source: ScrapeSource): GQL.ScraperSourceInput {
  if (source.kind === "scraper") return { scraper_id: source.id };
  return { stash_box_endpoint: source.endpoint };
}
