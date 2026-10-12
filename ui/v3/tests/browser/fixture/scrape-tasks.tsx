import { ScrapeTasks } from "@/components/settings/tasks/scrape-tasks";
import { ConfigurationProvider } from "@/hooks/config";
import {
  playerConfiguration,
  sourceTrackingConfiguration,
} from "./player-configuration";

export function ScrapeTasksFixture() {
  const off = new URLSearchParams(location.search).has("tracking-off");
  return (
    <ConfigurationProvider
      configuration={off ? playerConfiguration : sourceTrackingConfiguration}
    >
      <div className="max-w-3xl p-6">
        <ScrapeTasks />
      </div>
    </ConfigurationProvider>
  );
}
