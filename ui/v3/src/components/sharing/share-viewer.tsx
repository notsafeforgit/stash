import { useEffect, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { ShareBrowserView } from "./share-browser";
import { useShareNavigation } from "./share-browser-state";
import { useToast } from "@/hooks/toast";
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
  | { kind: "ready"; content: SharedContent; deadline: number; scope: string }
  | { kind: "unavailable" }
  | { kind: "error" };

async function openShare(
  target: ReturnType<typeof shareTarget>,
): Promise<SharedContent> {
  if (!target) throw new ShareUnavailableError();
  const secret = target.secret;
  const cleanURL = new URL(location.href);
  cleanURL.hash = "";
  if (secret) cleanURL.searchParams.set("key", secret);
  history.replaceState(history.state, "", cleanURL.pathname + cleanURL.search);
  const contentURL = shareContentURL(target.base);
  if (secret) {
    // The address-bar key survives reloads. Reuse a valid guest cookie instead
    // of creating another session and counting another recipient exchange.
    try {
      return await shareRequest(contentURL, shareContentSchema);
    } catch (error) {
      if (!(error instanceof ShareUnavailableError)) throw error;
    }
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
  return shareRequest(contentURL, shareContentSchema);
}

export function ShareViewer() {
  const intl = useIntl();
  const toast = useToast();
  const [loadingMore, setLoadingMore] = useState(false);
  const contentRequest = useRef<AbortController | null>(null);
  useEffect(() => () => contentRequest.current?.abort(), []);
  const [target] = useState(() => shareTarget(location.href, document.baseURI));
  const [navigationBase] = useState(
    () => target?.navigationBase ?? new URL(document.baseURI),
  );
  const navigation = useShareNavigation(navigationBase);
  const scope = navigation.location.gallery ?? "";
  const pages = useRef(new Map<string, SharedContent>());
  const [state, setState] = useState<ViewerState>({ kind: "loading" });
  const bootstrap = useRef<{
    attempt: number;
    promise: Promise<SharedContent>;
  } | null>(null);
  const [attempt, setAttempt] = useState(0);
  async function loadContent(refresh: boolean) {
    if (!target || state.kind !== "ready" || contentRequest.current) return;
    const controller = new AbortController();
    contentRequest.current = controller;
    setLoadingMore(true);
    try {
      const url = shareContentURL(target.base);
      if (!refresh && state.content.next)
        url.searchParams.set("cursor", state.content.next);
      const page = await shareRequest(
        url,
        shareContentSchema,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setState((current) => {
        if (current.kind !== "ready") return current;
        const content = refresh
          ? page
          : mergeShareContent(current.content, page);
        pages.current.set(scope, content);
        return { kind: "ready", content, deadline: shareDeadline(page), scope };
      });
    } catch (error) {
      if (!controller.signal.aborted) {
        if (error instanceof ShareUnavailableError)
          setState({ kind: "unavailable" });
        else toast.error(error);
      }
    } finally {
      if (!controller.signal.aborted) {
        contentRequest.current = null;
        setLoadingMore(false);
      }
    }
  }
  useEffect(() => {
    let active = true;
    if (bootstrap.current?.attempt !== attempt)
      bootstrap.current = { attempt, promise: openShare(target) };
    void bootstrap.current.promise.then(
      (content) => {
        if (active) {
          pages.current.set(
            new URLSearchParams(location.search).get("gallery") ?? "",
            content,
          );
          setState({
            kind: "ready",
            content,
            deadline: shareDeadline(content),
            scope: new URLSearchParams(location.search).get("gallery") ?? "",
          });
        }
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

  const ready = state.kind === "ready";
  const focused = navigation.location.media;
  useEffect(() => {
    if (!ready || !target) return;
    contentRequest.current?.abort();
    contentRequest.current = null;
    const cached = pages.current.get(scope);
    if (
      cached &&
      (!focused || cached.media.some((item) => item.key === focused))
    ) {
      setState((current) =>
        current.kind === "ready"
          ? { ...current, content: cached, scope }
          : current,
      );
      setLoadingMore(false);
      return;
    }
    const controller = new AbortController();
    contentRequest.current = controller;
    setLoadingMore(true);
    void shareRequest(
      shareContentURL(target.base),
      shareContentSchema,
      controller.signal,
    )
      .then((page) => {
        if (controller.signal.aborted) return;
        // A deep item link may need one focused item without resetting the
        // overview's existing continuation or forgetting earlier pages.
        const content = cached
          ? { ...mergeShareContent(cached, page), next: cached.next }
          : page;
        pages.current.set(scope, content);
        // Retain the overview for Back, plus a few recently visited galleries.
        for (const key of pages.current.keys()) {
          if (pages.current.size <= 4) break;
          if (key && key !== scope) pages.current.delete(key);
        }
        setState({
          kind: "ready",
          content,
          deadline: shareDeadline(page),
          scope,
        });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) {
          if (error instanceof ShareUnavailableError)
            setState({ kind: "unavailable" });
          else setState({ kind: "error" });
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          contentRequest.current = null;
          setLoadingMore(false);
        }
      });
    return () => controller.abort();
  }, [scope, focused, ready, target]);

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

  return (
    <ShareBrowserView
      navigation={navigation}
      busy={
        state.scope !== scope ||
        (loadingMore &&
          !!focused &&
          !state.content.media.some((item) => item.key === focused))
      }
      content={state.content}
      base={target.base}
      navigationBase={target.navigationBase}
      controls={
        <div className="flex shrink-0 items-center gap-2 px-4 pb-2 sm:px-6">
          <Button
            variant="ghost"
            size="sm"
            disabled={loadingMore || state.scope !== scope}
            onClick={() => void loadContent(true)}
          >
            <RefreshCw data-icon="inline-start" />
            {intl.formatMessage({
              id: "actions.refresh",
              defaultMessage: "Refresh",
            })}
          </Button>
          {state.content.next && (
            <Button
              variant="outline"
              size="sm"
              disabled={loadingMore || state.scope !== scope}
              onClick={() => void loadContent(false)}
            >
              {loadingMore && <Spinner data-icon="inline-start" />}
              {intl.formatMessage({
                id: "actions.load_more",
                defaultMessage: "Load more",
              })}
            </Button>
          )}
        </div>
      }
    />
  );
}

function shareContentURL(base: URL) {
  const url = new URL("content", base);
  const current = new URLSearchParams(location.search);
  for (const key of ["gallery", "media"]) {
    const value = current.get(key);
    if (value) url.searchParams.set(key, value);
  }
  return url;
}

export function mergeShareContent(
  current: SharedContent,
  page: SharedContent,
): SharedContent {
  const entries = new Map(
    current.entries.map((entry, index) => [entry.key ?? String(index), entry]),
  );
  for (const [index, entry] of page.entries.entries()) {
    const key = entry.key ?? String(index);
    const previous = entries.get(key);
    entries.set(key, {
      ...entry,
      media_keys: [
        ...new Set([...(previous?.media_keys ?? []), ...entry.media_keys]),
      ],
    });
  }
  return {
    ...page,
    entries: [...entries.values()],
    media: [
      ...new Map(
        [...current.media, ...page.media].map((item) => [item.key, item]),
      ).values(),
    ],
  };
}
