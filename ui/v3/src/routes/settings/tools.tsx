import { getPlatformURL } from "@/core/platform-url";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMsg } from "src/hooks/message";
import { Button } from "src/components/ui/button";
import {
  SettingDisplay,
  SettingsSection,
} from "src/components/settings/setting-row";

function SettingsToolsPage() {
  const msg = useMsg();

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection title={msg("config.tools.heading", "Tools")}>
        <SettingDisplay
          label={msg("config.tools.graphql_playground", "GraphQL playground")}
          description="Interactive query console for the GraphQL API."
          actions={
            <a
              href={getPlatformURL("playground").href}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Button type="button" variant="outline">
                {msg("actions.open", "Open")}
              </Button>
            </a>
          }
        />
      </SettingsSection>

      <SettingsSection title={msg("config.tools.scene_tools", "Scene tools")}>
        <SettingDisplay
          label={msg(
            "config.tools.scene_duplicate_checker",
            "Scene duplicate checker",
          )}
          description="Find visually duplicate scenes by perceptual hash."
          actions={
            <Link to="/scene-duplicate-checker">
              <Button type="button" variant="outline">
                {msg("actions.open", "Open")}
              </Button>
            </Link>
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
