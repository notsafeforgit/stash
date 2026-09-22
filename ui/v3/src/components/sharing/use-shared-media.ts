import { useEffect, useState } from "react";
import {
  shareDetailSchema,
  shareRequest,
  type SharedDetail,
} from "./share-contract";

type DetailState =
  | { kind: "loading" }
  | { kind: "ready"; detail: SharedDetail }
  | { kind: "error" };

export function useSharedMedia(mediaKey: string, base: URL): DetailState {
  const [result, setResult] = useState<{ key: string; state: DetailState }>();
  const key = new URL(`media/${mediaKey}/`, base).href;
  useEffect(() => {
    const abort = new AbortController();
    void shareRequest(new URL(key), shareDetailSchema, abort.signal).then(
      (detail) => {
        if (!abort.signal.aborted)
          setResult({ key, state: { kind: "ready", detail } });
      },
      () => {
        if (!abort.signal.aborted) setResult({ key, state: { kind: "error" } });
      },
    );
    return () => abort.abort();
  }, [key]);
  return result?.key === key ? result.state : { kind: "loading" };
}
