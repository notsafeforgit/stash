import { useEffect, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { Clock, Images, Play, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { SharedMediaViewer } from "./shared-media-viewer";
import {
  ShareUnavailableError,
  shareContentSchema,
  shareDeadline,
  shareRequest,
  shareStatusSchema,
  shareTarget,
  type SharedContent,
} from "./share-contract";

type ViewerState =
  | { kind: "loading" }
  | { kind: "ready"; content: SharedContent; deadline: number }
  | { kind: "unavailable" }
  | { kind: "error" };

async function openShare(
  target: ReturnType<typeof shareTarget>,
): Promise<SharedContent> {
  if (!target) throw new ShareUnavailableError();
  const secret = target.secret;
  history.replaceState(null, "", location.pathname);
  if (secret) {
    const response = await fetch(new URL("exchange", target.base), {
      method: "POST",
      credentials: "same-origin",
      redirect: "error",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret }),
    });
    if (response.status === 404) throw new ShareUnavailableError();
    if (!response.ok) throw new Error("Unable to open share");
    target.secret = "";
  }
  return shareRequest(new URL("content", target.base), shareContentSchema);
}

export function ShareViewer() {
  const intl = useIntl();
  const [target] = useState(() => shareTarget(location.href, document.baseURI));
  const [state, setState] = useState<ViewerState>({ kind: "loading" });
  const [selection, setSelection] = useState<string | null>(null);
  const bootstrap = useRef<{
    attempt: number;
    promise: Promise<SharedContent>;
  } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    if (bootstrap.current?.attempt !== attempt)
      bootstrap.current = { attempt, promise: openShare(target) };
    void bootstrap.current.promise.then(
      (content) => {
        if (active)
          setState({
            kind: "ready",
            content,
            deadline: shareDeadline(content),
          });
      },
      (error: unknown) => {
        if (active)
          setState({
            kind:
              error instanceof ShareUnavailableError ? "unavailable" : "error",
          });
      },
    );
    return () => {
      active = false;
    };
  }, [target, attempt]);

  const deadline = state.kind === "ready" ? state.deadline : null;
  useEffect(() => {
    if (deadline === null || !target) return;
    const abort = new AbortController();
    const timeout = window.setTimeout(
      () => {
        if (Date.now() >= deadline) setState({ kind: "unavailable" });
      },
      Math.min(2_147_483_647, Math.max(0, deadline - Date.now())),
    );
    const check = async () => {
      try {
        const status = await shareRequest(
          new URL("status", target.base),
          shareStatusSchema,
          abort.signal,
        );
        if (!abort.signal.aborted)
          setState((current) =>
            current.kind === "ready"
              ? { ...current, deadline: shareDeadline(status) }
              : current,
          );
      } catch (error) {
        if (!abort.signal.aborted)
          setState({
            kind:
              error instanceof ShareUnavailableError ? "unavailable" : "error",
          });
      }
    };
    const interval = window.setInterval(() => void check(), 15000);
    const visible = () => {
      if (!document.hidden) void check();
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      abort.abort();
      window.clearTimeout(timeout);
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [deadline, target]);

  if (state.kind !== "ready" || !target)
    return (
      <main className="flex min-h-dvh items-center justify-center p-6">
        {state.kind === "loading" ? (
          <Spinner />
        ) : (
          <Alert className="max-w-md">
            <AlertTitle>
              {intl.formatMessage({
                id: "sharing.unavailable",
                defaultMessage: "Share unavailable",
              })}
            </AlertTitle>
            <AlertDescription>
              {state.kind === "error"
                ? intl.formatMessage({
                    id: "sharing.connection_error",
                    defaultMessage:
                      "Unable to connect. Try opening the link again.",
                  })
                : intl.formatMessage({
                    id: "sharing.unavailable_description",
                    defaultMessage:
                      "This link has expired, was revoked, or is invalid.",
                  })}
            </AlertDescription>
            {state.kind === "error" && (
              <Button
                variant="outline"
                onClick={() => {
                  bootstrap.current = null;
                  setState({ kind: "loading" });
                  setAttempt((value) => value + 1);
                }}
              >
                <RefreshCw data-icon="inline-start" />
                {intl.formatMessage({
                  id: "actions.retry",
                  defaultMessage: "Retry",
                })}
              </Button>
            )}
          </Alert>
        )}
      </main>
    );

  const { content } = state;
  const selectedKey =
    selection ?? (content.media.length === 1 ? content.media[0]?.key : null);
  const index = content.media.findIndex((media) => media.key === selectedKey);
  const mediaByKey = new Map(content.media.map((media) => [media.key, media]));
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-6xl flex-col gap-6 p-4 pb-12 sm:p-8">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <h1 className="text-2xl font-semibold">{content.label}</h1>
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Clock className="size-4" />
          {intl.formatMessage(
            { id: "sharing.expires_on", defaultMessage: "Expires {date}" },
            {
              date: intl.formatDate(content.expires_at, {
                dateStyle: "medium",
                timeStyle: "short",
              }),
            },
          )}
        </p>
      </header>
      {selectedKey && (
        <SharedMediaViewer
          key={selectedKey}
          mediaKey={selectedKey}
          base={target.base}
          previous={
            index > 0
              ? () => setSelection(content.media[index - 1]?.key ?? null)
              : undefined
          }
          next={
            index < content.media.length - 1
              ? () => setSelection(content.media[index + 1]?.key ?? null)
              : undefined
          }
        />
      )}
      {content.media.length > 1 &&
        content.entries.map((entry, entryIndex) => (
          <section
            key={`${entry.kind}-${entryIndex}`}
            className="flex flex-col gap-3"
          >
            {entry.kind === "GALLERY" && (
              <h2 className="flex items-center gap-2 text-lg font-medium">
                <Images className="size-5" />
                {entry.title ||
                  intl.formatMessage({
                    id: "gallery",
                    defaultMessage: "Gallery",
                  })}
              </h2>
            )}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              {entry.media_keys.map((key) => {
                const media = mediaByKey.get(key);
                if (!media) return null;
                const title =
                  media.title ||
                  intl.formatMessage({
                    id: media.kind === "SCENE" ? "scene" : "image",
                    defaultMessage: media.kind === "SCENE" ? "Scene" : "Image",
                  });
                return (
                  <Card key={key} className="overflow-hidden py-0">
                    <CardContent className="p-0">
                      <Button
                        variant="ghost"
                        className="h-auto w-full flex-col gap-0 p-0"
                        onClick={() => setSelection(key)}
                        aria-label={title}
                        aria-pressed={selectedKey === key}
                      >
                        <span className="relative block aspect-video w-full overflow-hidden">
                          <img
                            src={media.thumbnail}
                            alt=""
                            loading="lazy"
                            className="h-full w-full object-cover"
                          />
                          {media.video && (
                            <Play className="absolute left-2 top-2 size-5" />
                          )}
                        </span>
                        <span className="w-full truncate p-3 text-left">
                          {title}
                        </span>
                      </Button>
                    </CardContent>
                  </Card>
                );
              })}
            </div>
          </section>
        ))}
    </main>
  );
}
