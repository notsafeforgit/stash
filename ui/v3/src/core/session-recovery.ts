import { applicationBaseURL } from "./platform-url";

export class SessionRecoveryUnavailable extends Error {
  constructor() {
    super("Sign in to continue this request.");
  }
}

/** One prompt coordinates all transports without replacing the page or drafts. */
export class SessionRecovery {
  private listeners = new Set<() => void>();
  private waiters = new Map<
    symbol,
    { resolve: () => void; cleanup: () => void }
  >();
  generation = 0;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.waiters.size > 0;
  get available() {
    return this.listeners.size > 0;
  }
  private notify() {
    for (const listener of this.listeners) listener();
  }

  wait(signal?: AbortSignal | null): Promise<void> {
    if (!this.available)
      return Promise.reject(new SessionRecoveryUnavailable());
    if (signal?.aborted) return Promise.reject(signal.reason);
    return new Promise<void>((resolve, reject) => {
      const id = Symbol();
      const abort = () => {
        this.waiters.delete(id);
        reject(signal?.reason);
        this.notify();
      };
      this.waiters.set(id, {
        resolve,
        cleanup: () => signal?.removeEventListener("abort", abort),
      });
      signal?.addEventListener("abort", abort, { once: true });
      this.notify();
    });
  }

  authenticated = () => {
    this.generation++;
    for (const waiter of this.waiters.values()) {
      waiter.cleanup();
      waiter.resolve();
    }
    this.waiters.clear();
    this.notify();
  };
}

export const sessionRecovery = new SessionRecovery();

function isOwnerSessionRequest(target: string | URL): boolean {
  if (typeof window === "undefined") return false;
  const base = applicationBaseURL();
  const url = new URL(target, base);
  return (
    url.origin === base.origin &&
    !url.username &&
    !url.password &&
    (url.pathname === `${base.pathname}graphql` ||
      url.pathname.startsWith(`${base.pathname}api/v3/archive/`))
  );
}

/** Only an explicit authentication rejection is replayable. Network failures,
 * server errors and conflicts retain their existing outcome/recovery semantics. */
export async function withSessionRecovery(
  request: () => Promise<Response>,
  target: string | URL,
  signal?: AbortSignal | null,
  recovery = sessionRecovery,
): Promise<Response> {
  const generation = recovery.generation;
  const response = await request();
  if (
    response.status !== 401 ||
    !isOwnerSessionRequest(target) ||
    !recovery.available
  )
    return response;
  await response.body?.cancel();
  if (recovery.generation === generation) await recovery.wait(signal);
  signal?.throwIfAborted();
  // At most one retry, using the caller's exact saved body and request identity.
  return request();
}

export const sessionFetch: typeof fetch = async (input, init) => {
  if (input instanceof Request || init?.body instanceof ReadableStream) {
    const request = new Request(input, init);
    return withSessionRecovery(
      () => fetch(request.clone()),
      request.url,
      request.signal,
    );
  }
  // UploadHttpLink supplies a URL and a string/FormData body. Preserve immutable
  // File references rather than teeing an upload stream into an unread clone,
  // which would buffer the entire media upload while the first request runs.
  let body = init?.body;
  if (body instanceof FormData) {
    const copied = new FormData();
    body.forEach((value, key) => {
      copied.append(key, value);
    });
    body = copied;
  } else if (body instanceof URLSearchParams) body = new URLSearchParams(body);
  else if (body instanceof ArrayBuffer) body = body.slice(0);
  else if (ArrayBuffer.isView(body))
    body = new Uint8Array(
      body.buffer,
      body.byteOffset,
      body.byteLength,
    ).slice();
  const target = String(input);
  const options = { ...init, headers: new Headers(init?.headers), body };
  return withSessionRecovery(
    () => fetch(target, options),
    target,
    options.signal,
  );
};
