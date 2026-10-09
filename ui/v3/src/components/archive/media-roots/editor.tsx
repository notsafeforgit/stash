import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  mediaRootInputSchema,
  type MediaRoot,
  type MediaRootAPI,
  type MediaRootInput,
} from "@/core/native-archive/media-root-api";
import {
  createMediaRootOutbox,
  type SavedMediaRoot,
} from "@/core/native-archive/media-root-outbox";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { MediaRootForm, MediaRootError } from "./form";
import { MediaRootHistory } from "./history";

export function MediaRootEditor({
  api,
  id,
  create,
  onBack,
  onChanged,
}: {
  api: MediaRootAPI;
  id: string;
  create: boolean;
  onBack: () => void;
  onChanged: (root: MediaRoot) => void;
}) {
  const msg = useMsg();
  const [outbox] = useState(() => createMediaRootOutbox(api));
  const [data, setData] = useState<{
    input: MediaRootInput;
    current?: MediaRoot;
  }>();
  const [saved, setSaved] = useState<SavedMediaRoot | null>(null);
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Refresh loads the selected root and its saved request.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setReady(false);
      setError(undefined);
      try {
        const pending = await outbox.read(id);
        if (controller.signal.aborted) return;
        setSaved(pending);
        let current: MediaRoot | undefined;
        try {
          current = await api.root(id, controller.signal);
        } catch (error) {
          if (
            !(
              create &&
              error instanceof NativeArchiveError &&
              error.status === 404
            )
          )
            throw error;
        }
        const input = pending
          ? mediaRootInputSchema.parse(JSON.parse(pending.body))
          : ({
              uuid: id,
              expected_revision: current?.revision ?? 0,
              label: current?.label ?? "",
              state: current?.state ?? "active",
              binding: current?.binding ?? null,
              reason: "",
            } satisfies MediaRootInput);
        if (!controller.signal.aborted) {
          setData({ input, current });
          setReady(true);
          if (current) onChanged(current);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, id, create, onChanged, refresh]);
  function reload() {
    setReady(false);
    setBusy(true);
    setRefresh((value) => value + 1);
  }
  async function deliver(input?: MediaRootInput) {
    if (busy || (input && (!ready || saved))) return;
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (input) setSaved(await outbox.prepare(input));
      await outbox.deliver(id);
      setApplied(true);
      setReady(false);
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
    } finally {
      try {
        setSaved(await outbox.read(id));
      } catch (error) {
        setReady(false);
        setError(error);
      }
      setBusy(false);
    }
  }
  async function reviewAgain() {
    if (busy || saved?.state !== "rejected") return;
    setBusy(true);
    setReady(false);
    setError(undefined);
    try {
      await outbox.forgetRejected(id, saved.body);
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <Button type="button" variant="ghost" className="w-fit" onClick={onBack}>
        <ArrowLeft data-icon="inline-start" />
        {msg("media_roots.back", "Back to media roots")}
      </Button>
      {busy && <Spinner />}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("media_roots.saved", "Media root saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "media_roots.saved_help",
              "The archive has recorded this root definition and its binding history.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "media_roots.refresh_failed",
                "The root is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={reload}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <MediaRootError error={error} retry={busy ? undefined : reload} />
        ))}
      {saved && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg("media_roots.pending", "Confirm this saved root change")
              : msg(
                  "media_roots.conflict",
                  "Review the root and folder before saving again",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {saved.state === "pending"
                ? msg(
                    "media_roots.pending_help",
                    "This browser has an unconfirmed save. Check the recorded revision and retry the same checked binding if needed.",
                  )
                : msg(
                    "media_roots.conflict_help",
                    "The root changed, or its definition or checked folder was rejected. Reload the current definition and check any replacement folder before saving a new change.",
                  )}
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() =>
                void (saved.state === "pending" ? deliver() : reviewAgain())
              }
            >
              {saved.state === "pending"
                ? msg("media_roots.recover", "Check and retry root change")
                : msg("media_roots.reload", "Review current root")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {data && (
        <Card>
          <CardHeader>
            <CardTitle>
              {data.current?.label ?? msg("media_roots.new", "New media root")}
            </CardTitle>
            <CardDescription>
              {msg(
                "media_roots.editor_help",
                "A stable root identity connects portable collection paths to a folder on this server.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {data.current?.state === "retired" && (
              <p>
                {msg(
                  "media_roots.retired_help",
                  "This root is retired. Choose Active or Disabled and save to restore it with the same identity and history. Activating it verifies its folder again.",
                )}
              </p>
            )}
            <MediaRootForm
              key={`${refresh}:${data.current?.revision ?? 0}`}
              api={api}
              input={data.input}
              disabled={!ready || busy || !!saved}
              onSave={deliver}
            />
            {data.current && (
              <MediaRootHistory key={data.current.revision} api={api} id={id} />
            )}
            <p data-selectable-text className="wrap-anywhere font-mono text-xs">
              {id}
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
