import { ProviderMetadataHistory } from "./provider-metadata-history";
import { useEffect, useState } from "react";
import { useMsg } from "@/hooks/message";
import {
  createMetadataReviewAPI,
  type MetadataFields,
} from "@/core/native-archive/metadata-review-api";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError, type EntityNames } from "./native-metadata/shared";
import { FieldSummary } from "./native-metadata/fields";

/** Key this component by kind/local ID so navigation cannot retain another
 * entity's pending query or result. */
export function NativeMetadataReview({
  kind,
  entity,
}: {
  kind: "scene" | "image";
  entity: EntityNames;
}) {
  const msg = useMsg();
  const [api] = useState(() => createMetadataReviewAPI());
  const [fields, setFields] = useState<MetadataFields>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: The refresh counter explicitly reloads after a requested retry.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setError(undefined);
      try {
        const identity = await api.identity(kind, entity.id, controller.signal);
        const state = await api.fields(identity.uuid, controller.signal);
        if (!controller.signal.aborted) setFields(state);
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, kind, entity.id, refresh]);

  return (
    <section
      aria-label={msg("archive_review.title", "Metadata review")}
      className="flex flex-col gap-4"
    >
      {kind === "scene" && (
        <ProviderMetadataHistory kind="scene" libraryId={entity.id} />
      )}
      <p className="text-sm text-muted-foreground">
        {msg(
          "archive_review.intro",
          "Current field choices for this item, with the reason each was recorded.",
        )}
      </p>
      {busy && (
        <Spinner
          aria-label={msg("archive_review.loading", "Loading metadata review")}
        />
      )}
      {error !== undefined && (
        <ReviewError
          error={error}
          retry={() => setRefresh((value) => value + 1)}
        />
      )}
      {fields && <FieldSummary fields={fields} entity={entity} />}
    </section>
  );
}
