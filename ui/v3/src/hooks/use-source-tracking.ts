import { useConfigurationContextOptional } from "@/hooks/config";

// Use the running capability, not a saved change awaiting server restart.
export function useSourceTracking() {
  return (
    useConfigurationContextOptional()?.configuration.general
      .sourceTrackingActive === true
  );
}
