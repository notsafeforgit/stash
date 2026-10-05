import { z } from "zod";
import { applicationBaseURL, joinPlatformURL } from "../platform-url";

export class NativeArchiveError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(`Native archive request failed (${status}, ${code})`);
  }
}

export function nativeArchiveEndpoint(base = applicationBaseURL()): string {
  const endpoint = joinPlatformURL(base, "api/v3/archive/");
  if (
    !/^https?:$/.test(endpoint.protocol) ||
    endpoint.username ||
    endpoint.password
  )
    throw new NativeArchiveError(0, "invalid_endpoint");
  endpoint.search = "";
  endpoint.hash = "";
  return endpoint.href;
}

/** Application sessions and public mounts are shared by native review clients.
 * Website credentials and producer tokens never enter this transport. */
export function createArchiveRequest(
  endpoint: string,
  transport: typeof fetch,
) {
  return async function request<T>(
    path: string,
    schema: z.ZodType<T>,
    body?: string,
    signal?: AbortSignal,
    method: "GET" | "POST" | "PUT" = body === undefined ? "GET" : "POST",
  ): Promise<T> {
    const response = await transport(new URL(path, endpoint), {
      method,
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      headers:
        body === undefined
          ? { Accept: "application/json" }
          : { Accept: "application/json", "Content-Type": "application/json" },
      body,
      signal,
    });
    if (!response.ok) {
      const result = z
        .object({ error: z.string() })
        .safeParse(await response.json().catch(() => null));
      throw new NativeArchiveError(
        response.status,
        result.success ? result.data.error : "request_failed",
      );
    }
    const result = schema.safeParse(await response.json());
    if (!result.success)
      throw new NativeArchiveError(response.status, "invalid_response");
    return result.data;
  };
}
