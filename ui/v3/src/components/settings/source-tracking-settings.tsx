import { useConfigurationContext, useConfigureGeneral } from "@/hooks/config";
import { useMsg } from "@/hooks/message";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { SettingsSection, SettingSwitch } from "./setting-row";

export function SourceTrackingSettings() {
  const msg = useMsg();
  const { configuration } = useConfigurationContext();
  const { sourceTrackingEnabled, sourceTrackingActive } = configuration.general;
  const [configure, { loading }] = useConfigureGeneral();
  return (
    <SettingsSection
      title={msg("source_tracking.title", "Source tracking")}
      description={msg(
        "source_tracking.description",
        "Optional tools for tracking website posts, accounts, albums and imports from gallery-dl or other producers. Download workers and schedules require separate setup. Local library scanning and metadata providers work independently.",
      )}
    >
      <SettingSwitch
        label={msg("source_tracking.enable", "Enable source tracking")}
        description={msg(
          "source_tracking.restart_help",
          "Off by default. Changes take effect after restarting Stash. Turning this off preserves imported media, metadata and queued work.",
        )}
        checked={sourceTrackingEnabled}
        disabled={loading}
        onChange={(sourceTrackingEnabled) => {
          void configure({ variables: { input: { sourceTrackingEnabled } } });
        }}
      />
      {sourceTrackingActive !== sourceTrackingEnabled && (
        <Alert>
          <AlertTitle>
            {msg("source_tracking.restart_required", "Restart required")}
          </AlertTitle>
          <AlertDescription>
            {sourceTrackingActive
              ? msg(
                  "source_tracking.pending_disable",
                  "Source tracking is still running. Restart Stash to turn it off.",
                )
              : msg(
                  "source_tracking.pending_enable",
                  "Source tracking is still off. Restart Stash to turn it on.",
                )}
          </AlertDescription>
        </Alert>
      )}
    </SettingsSection>
  );
}
