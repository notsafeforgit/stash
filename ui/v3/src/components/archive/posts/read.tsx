import { useEffect, useRef, useState, type ReactNode } from "react";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { PostReadError, PostEmpty } from "./shared";

export function usePostRead<T>(load: (signal: AbortSignal) => Promise<T>) {
  const [value, setValue] = useState<T>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(true);
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry explicitly reloads the same resource.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    void load(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setValue(value);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [load, refresh]);
  return { value, error, busy, retry: () => setRefresh((n) => n + 1) };
}

export function PostRows<T>({
  load,
  rowKey,
  renderRow,
  empty,
  pageLimit,
}: {
  load: (after?: string, signal?: AbortSignal) => Promise<T[]>;
  rowKey: (row: T) => string;
  renderRow: (row: T) => ReactNode;
  empty: string;
  pageLimit: number;
}) {
  const msg = useMsg();
  const [rows, setRows] = useState<T[]>([]);
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(true);
  const [more, setMore] = useState(false);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    void load(undefined, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setRows(page);
        setMore(page.length === pageLimit);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => {
      controller.abort();
      pending.current?.abort();
    };
  }, [load, pageLimit]);
  async function loadMore() {
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      const last = rows.at(-1);
      const page = await load(
        last ? rowKey(last) : undefined,
        controller.signal,
      );
      if (!controller.signal.aborted) {
        setRows((prior) => [...prior, ...page]);
        setMore(page.length === pageLimit);
      }
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      {busy && (
        <Spinner
          aria-label={msg(
            "source_posts.loading_details",
            "Loading post details",
          )}
        />
      )}
      {error !== undefined && (
        <PostReadError error={error} retry={() => void loadMore()} />
      )}
      {!busy && error === undefined && !rows.length && (
        <PostEmpty title={empty} />
      )}
      {rows.map((row) => (
        <div key={rowKey(row)}>{renderRow(row)}</div>
      ))}
      {more && (
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
