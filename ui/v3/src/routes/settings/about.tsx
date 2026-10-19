import { createFileRoute } from "@tanstack/react-router";
import { FormattedMessage } from "react-intl";
import { useMsg } from "src/hooks/message";
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

  const msg = useMsg();

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
            <FormattedMessage
              id="config.about.project_description"
              defaultMessage="Native Stash is a standalone media library, hosted on {repository}."
              values={{
                repository: (
                  <ExternalLink href="https://github.com/notsafeforgit/stash">
                    GitHub
                  </ExternalLink>
                ),
              }}
            />
          </p>
          <p>
            <FormattedMessage
              id="config.about.support_links"
              defaultMessage="Guides and support: {documentation} and {issues}."
              values={{
                documentation: (
                  <ExternalLink href="https://github.com/notsafeforgit/stash/blob/develop/docs/README.md">
                    {msg("config.about.documentation", "Documentation")}
                  </ExternalLink>
                ),
                issues: (
                  <ExternalLink href="https://github.com/notsafeforgit/stash/issues">
                    {msg("config.about.issues", "Issues")}
                  </ExternalLink>
                ),
              }}
            />
          </p>
          <p>
            <FormattedMessage
              id="config.about.attribution"
              defaultMessage="Derived from {upstream}, under the {license} license."
              values={{
                upstream: (
                  <ExternalLink href="https://github.com/stashapp/stash">
                    stashapp/stash
                  </ExternalLink>
                ),
                license: (
                  <ExternalLink href="https://github.com/notsafeforgit/stash/blob/develop/LICENSE">
                    AGPL-3.0
                  </ExternalLink>
                ),
              }}
            />
          </p>
        </div>
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/about")({
  component: SettingsAboutPage,
});
