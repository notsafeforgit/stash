import { useMsg } from "@/hooks/message";
import { NativeArchiveError } from "@/core/native-archive/client";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export function PolicyError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const msg = useMsg();
  if (
    !(error instanceof NativeArchiveError) ||
    (error.code !== "invalid_policy" && error.code !== "preview_changed")
  )
    return <ReviewError error={error} retry={retry} />;
  const invalid = error.code === "invalid_policy";
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {invalid
          ? msg("metadata_policy.invalid", "Check the metadata rules")
          : msg("metadata_policy.changed", "The collection or policy changed")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {invalid
            ? msg(
                "metadata_policy.invalid_help",
                "A mapping expression, value or referenced entity could not be accepted. Correct the draft before trying again.",
              )
            : msg(
                "metadata_policy.changed_help",
                "Review the latest collection and policy before saving or testing this draft again.",
              )}
        </p>
        {invalid && error.detail && (
          <p data-selectable-text className="whitespace-pre-wrap wrap-anywhere">
            {error.detail}
          </p>
        )}
        {retry && (
          <Button type="button" variant="outline" onClick={retry}>
            {msg("actions.retry", "Retry")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}
