import { useEffect, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { ShareBrowser } from "./share-browser";
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
  history.replaceState(history.state, "", location.pathname + location.search);
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

  return <ShareBrowser content={state.content} base={target.base} />;
}
