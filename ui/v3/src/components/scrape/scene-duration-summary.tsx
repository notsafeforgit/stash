import { CheckCircle2, Clock } from "lucide-react";
import { useIntl } from "react-intl";
import { Badge } from "@/components/ui/badge";
import { secondsToTimestamp } from "@/utils/duration";
import {
  compareSceneDuration,
  type DurationFile,
  type DurationSource,
} from "./scene-duration";

export function SceneDurationSummary({
  source,
  localFiles,
}: {
  source: DurationSource;
  localFiles?: readonly DurationFile[];
}) {
  const intl = useIntl();
  const comparison = compareSceneDuration(source, localFiles);
  if (!comparison) return null;
  const { remote, local, localCount, difference, matches } = comparison;
  return (
    <span className="inline-flex flex-col items-start gap-1 whitespace-normal">
      <span>
        {intl.formatMessage(
          {
            id: "scrape.remote_duration",
            defaultMessage: "Remote: {duration}",
          },
          { duration: secondsToTimestamp(remote) },
        )}
        {local !== undefined && (
          <>
            {" · "}
            {intl.formatMessage(
              localCount > 1
                ? {
                    id: "scrape.closest_duration",
                    defaultMessage: "Closest local file: {duration}",
                  }
                : {
                    id: "scrape.local_duration",
                    defaultMessage: "Local: {duration}",
                  },
              { duration: secondsToTimestamp(local) },
            )}
          </>
        )}
      </span>
      {difference !== undefined && (
        <Badge variant={matches ? "success" : "outline"}>
          {matches ? (
            <CheckCircle2 data-icon="inline-start" />
          ) : (
            <Clock data-icon="inline-start" />
          )}
          {matches
            ? intl.formatMessage({
                id: "scrape.duration_match",
                defaultMessage: "Duration matches (within 5 seconds)",
              })
            : intl.formatMessage(
                {
                  id: "scrape.duration_difference",
                  defaultMessage: "Duration differs by {seconds}s",
                },
                {
                  seconds: intl.formatNumber(difference, {
                    maximumFractionDigits: 1,
                  }),
                },
              )}
        </Badge>
      )}
    </span>
  );
}
