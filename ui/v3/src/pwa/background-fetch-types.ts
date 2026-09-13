/** Background Fetch is not in TypeScript's DOM lib. Keep its optional browser
 * boundary explicit; feature detection and request rejection both need fallbacks. */
export interface BackgroundFetchRecord {
  readonly request: Request;
  readonly responseReady: Promise<Response>;
}
export interface BackgroundFetchRegistration extends EventTarget {
  readonly id: string;
  readonly downloaded: number;
  readonly downloadTotal: number;
  readonly result: "" | "success" | "failure";
  readonly failureReason: string;
  readonly recordsAvailable: boolean;
  abort(): Promise<boolean>;
  matchAll(): Promise<BackgroundFetchRecord[]>;
}
export interface BackgroundFetchManager {
  fetch(
    id: string,
    requests: Request[],
    options: { title: string },
  ): Promise<BackgroundFetchRegistration>;
  get(id: string): Promise<BackgroundFetchRegistration | undefined>;
}
declare global {
  interface ServiceWorkerRegistration {
    readonly backgroundFetch?: BackgroundFetchManager;
  }
}
