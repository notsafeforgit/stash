import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useQuery } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import {
  SettingDisplay,
  SettingsSection,
} from "src/components/settings/setting-row";

function ExternalLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-primary underline-offset-2 hover:underline"
    >
      {children}
    </a>
  );
}

function SettingsAboutPage() {
  const intl = useIntl();

  const { data: versionData } = useQuery(GQL.VersionDocument);
  const {
    data: latestData,
    error: latestError,
    loading: latestLoading,
    refetch: refetchLatest,
  } = useQuery(GQL.LatestVersionDocument, {
    fetchPolicy: "network-only",
    notifyOnNetworkStatusChange: true,
  });

  const msg = (id: string, defaultMessage: string) =>
    intl.formatMessage({ id, defaultMessage });

  const version = versionData?.version;
  const latest = latestData?.latestversion;
  const isNewVersion =
    !!latest && !!version && latest.shorthash !== version.hash;

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection title={msg("config.about.version", "Version")}>
        <SettingDisplay
          label={version?.version || "—"}
          description={msg("config.about.build_hash", "Build hash")}
          value={version?.hash}
        />
        <SettingDisplay
          label={msg("config.about.build_time", "Build time")}
          value={version?.build_time}
        />
      </SettingsSection>

      <SettingsSection
        title={msg("config.about.latest_version", "Latest version")}
      >
        {latestLoading ? (
          <Spinner className="size-5" />
        ) : latestError ? (
          <p className="text-sm text-destructive">{latestError.message}</p>
        ) : latest ? (
          <SettingDisplay
            label={
              <>
                {latest.version}
                {isNewVersion && (
                  <span className="ml-2 text-primary">
                    {msg(
                      "config.about.new_version_notice",
                      "[NEW VERSION AVAILABLE!]",
                    )}
                  </span>
                )}
              </>
            }
            description={`${msg("config.about.release_date", "Release date")}: ${latest.release_date}`}
            value={latest.shorthash}
            actions={
              <>
                <a href={latest.url} target="_blank" rel="noopener noreferrer">
                  <Button type="button" variant="outline">
                    {msg("actions.download", "Download")}
                  </Button>
                </a>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => void refetchLatest()}
                >
                  {msg(
                    "config.about.check_for_new_version",
                    "Check for new version",
                  )}
                </Button>
              </>
            }
          />
        ) : null}
      </SettingsSection>

      <SettingsSection title={msg("config.categories.about", "About")}>
        <div className="space-y-2 text-sm text-muted-foreground">
          <p>
            Stash is open source, hosted on{" "}
            <ExternalLink href="https://github.com/stashapp/stash">
              GitHub
            </ExternalLink>
            .
          </p>
          <p>
            Guides and support:{" "}
            <ExternalLink href="https://docs.stashapp.cc">
              documentation
            </ExternalLink>
            ,{" "}
            <ExternalLink href="https://discourse.stashapp.cc">
              forum
            </ExternalLink>{" "}
            and{" "}
            <ExternalLink href="https://discord.gg/2TsNFKt">
              Discord
            </ExternalLink>
            .
          </p>
          <p>
            Support the project via{" "}
            <ExternalLink href="https://opencollective.com/stashapp">
              Open Collective
            </ExternalLink>{" "}
            or{" "}
            <ExternalLink href="https://github.com/sponsors/stashapp">
              GitHub Sponsors
            </ExternalLink>
            .
          </p>
        </div>
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/about")({
  component: SettingsAboutPage,
});
