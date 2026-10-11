import type { PropsWithChildren } from "react";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import { useSourceTracking } from "@/hooks/use-source-tracking";
import { isSourceTrackingPath } from "@/core/source-tracking";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export function SourceTrackingGate({
  routeId,
  children,
}: PropsWithChildren<{ routeId: string }>) {
  const enabled = useSourceTracking();
  const msg = useMsg();
  if (enabled || !isSourceTrackingPath(routeId)) return children;
  return (
    <div className="mx-auto w-full max-w-3xl p-4 md:p-6">
      <Alert>
        <AlertTitle>
          {msg("source_tracking.disabled", "Source tracking is off")}
        </AlertTitle>
        <AlertDescription>
          <p>
            {msg(
              "source_tracking.enable_help",
              "Enable source tracking in Settings → Services and restart Stash to use posts, accounts and source collections. Existing data is preserved while it is off.",
            )}
          </p>
          <Button variant="outline" render={<Link to="/settings/services" />}>
            {msg("source_tracking.open_settings", "Open service settings")}
          </Button>
        </AlertDescription>
      </Alert>
    </div>
  );
}
