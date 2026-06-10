import { createFileRoute, Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { Button } from "src/components/ui/button";
import {
  SettingDisplay,
  SettingsSection,
} from "src/components/settings/setting-row";

function SettingsToolsPage() {
  const intl = useIntl();
  const msg = (id: string, defaultMessage: string) =>
    intl.formatMessage({ id, defaultMessage });

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection title={msg("config.tools.heading", "Tools")}>
        <SettingDisplay
          label={msg("config.tools.graphql_playground", "GraphQL playground")}
          description="Interactive query console for the GraphQL API."
          actions={
            <a href="/playground" target="_blank" rel="noopener noreferrer">
              <Button type="button" variant="outline">
                {msg("actions.open", "Open")}
              </Button>
            </a>
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.tools.image_tools", "Image tools")}>
        <SettingDisplay
          label={msg(
            "config.tools.image_duplicate_checker",
            "Image duplicate checker",
          )}
          description="Find visually duplicate images by perceptual hash."
          actions={
            <Link to="/image-duplicate-checker">
              <Button type="button" variant="outline">
                {msg("actions.open", "Open")}
              </Button>
            </Link>
          }
        />
      </SettingsSection>
    </div>
  );
}

export const Route = createFileRoute("/settings/tools")({
  component: SettingsToolsPage,
});
