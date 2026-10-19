import { applicationBaseURL } from "./platform-url";

/** Return links cannot leave this application's origin or proxy mount. */
export function loginReturnURL(
  value: string | null,
  base = applicationBaseURL(),
): string {
  try {
    const url = new URL(value || base.href, base);
    if (
      url.origin === base.origin &&
      !url.username &&
      !url.password &&
      url.pathname.startsWith(base.pathname) &&
      !url.pathname.startsWith("//") &&
      url.pathname !== `${base.pathname}login`
    )
      return url.href;
  } catch {
    /* Invalid links return to the application root. */
  }
  return base.href;
}
