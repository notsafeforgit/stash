import { FormattedMessage } from "react-intl";
import { createFileRoute, Link } from "@tanstack/react-router";
import { getClient } from "@/core/client";
import { ConfigurationDocument } from "@/core/generated-graphql";
import {
  decodeTvSettings,
  tvSearchSchema,
  type TvFilterChoice,
} from "@/core/tv/settings";
import { resolveTvQuery } from "@/core/tv/feed-query";
import { TvPage } from "@/components/tv/tv-page";
import { Button, buttonVariants } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { createTvRotationStore } from "@/hooks/use-tv-settings";

export const Route = createFileRoute("/tv")({
  validateSearch: (search) => tvSearchSchema.parse(search),
  loaderDeps: ({ search }) => ({ search }),
  pendingMs: 600,
  pendingMinMs: 0,
  pendingComponent: () => (
    <div className="flex h-full items-center justify-center gap-4">
      <Spinner />
      <Link to="/scenes">
        <FormattedMessage
          id="tv.text.back_to_scenes"
          defaultMessage="Back to scenes"
        />
      </Link>
    </div>
  ),
  errorComponent: ({ error, reset }) => (
    <div className="flex flex-col items-start gap-4 p-6">
      <p role="alert">
        {error instanceof Error ? error.message : String(error)}
      </p>
      <Button onClick={reset}>
        <FormattedMessage id="tv.text.retry" defaultMessage="Retry" />
      </Button>
      <Link
        to="/settings/tv"
        className={buttonVariants({ variant: "outline" })}
      >
        <FormattedMessage
          id="tv.text.tv_settings"
          defaultMessage="TV settings"
        />
      </Link>
      <Link to="/scenes">
        <FormattedMessage
          id="tv.text.back_to_scenes"
          defaultMessage="Back to scenes"
        />
      </Link>
    </div>
  ),
  loader: async ({ deps }) => {
    const client = getClient();
    const result = await client.query({
      query: ConfigurationDocument,
      fetchPolicy: "cache-first",
    });
    if (!result.data) throw new Error("App configuration is unavailable");
    const config = result.data.configuration;
    const decoded = decodeTvSettings(config.ui.tv);
    if (decoded.kind !== "ready")
      throw new Error(
        "Recover TV settings in Settings → TV before opening the feed",
      );
    const { settings } = decoded;
    const { search } = deps;
    const mode = search.mode ?? settings.mode;
    if (
      search.item &&
      !search.item.startsWith(mode === "scenes" ? "scene:" : "marker:")
    )
      throw new Error("The selected item belongs to another TV feed");
    const choice: TvFilterChoice | undefined =
      search.filter === "default" || search.filter === "all"
        ? { kind: search.filter }
        : search.filter
          ? { kind: "saved" as const, id: search.filter }
          : undefined;
    const random = crypto.getRandomValues(new Uint32Array(1))[0] ?? 0;
    const seed = search.seed ?? random % 2147483647;
    const portrait = window.matchMedia("(orientation: portrait)").matches;
    const rotated = createTvRotationStore().getSnapshot() !== "normal";
    const orientation = portrait !== rotated ? "portrait" : "landscape";
    const query = await resolveTvQuery(
      client,
      config,
      settings,
      mode,
      choice,
      seed,
      orientation,
    );
    return { query, settings, seed, search };
  },
  component: () => {
    const props = Route.useLoaderData();
    return <TvPage {...props} />;
  },
});
