import { linkOptions } from "@tanstack/react-router";
import { applicationBaseURL, applicationHref } from "./platform-url";

/** Core destinations keep params/search separate so the generated router checks
 * every producer. Runtime plugin and history URLs use the boundary below. */
export const entityDestination = {
  scene: (sceneId: string, search?: { t?: number; tab?: string }) =>
    linkOptions({ to: "/scenes/$sceneId", params: { sceneId }, search }),
  image: (imageId: string) =>
    linkOptions({ to: "/images/$imageId", params: { imageId } }),
  gallery: (galleryId: string) =>
    linkOptions({ to: "/galleries/$galleryId", params: { galleryId } }),
  performer: (performerId: string) =>
    linkOptions({ to: "/performers/$performerId", params: { performerId } }),
  studio: (studioId: string) =>
    linkOptions({ to: "/studios/$studioId", params: { studioId } }),
  group: (groupId: string) =>
    linkOptions({ to: "/groups/$groupId", params: { groupId } }),
  tag: (tagId: string) =>
    linkOptions({ to: "/tags/$tagId", params: { tagId } }),
  offline: (sceneId: string) =>
    linkOptions({ to: "/offline/$sceneId", params: { sceneId } }),
};
export type EntityDestination = ReturnType<
  (typeof entityDestination)[keyof typeof entityDestination]
>;

/** Stored return URLs are untrusted. Accept application-relative paths or
 * same-origin public URLs within this deployment, and retain encoded search. */
export function localNavigationHref(value: string): string | undefined {
  const base = applicationBaseURL();
  if (!value.startsWith("/") && !/^https?:\/\//i.test(value)) return undefined;
  try {
    const url = new URL(value, base);
    if (url.origin !== base.origin) return undefined;
    const prefix = base.pathname.replace(/\/$/, "");
    const withinDeployment = (candidate: URL) =>
      candidate.pathname === prefix ||
      candidate.pathname.startsWith(`${prefix}/`);
    const isPublic = withinDeployment(url);
    if ((/^https?:\/\//i.test(value) || value.startsWith("//")) && !isPublic)
      return undefined;
    const href = isPublic
      ? url.pathname + url.search + url.hash
      : applicationHref(value, base);
    // URL normalization can resolve dot segments outside the public prefix.
    return withinDeployment(new URL(href, base)) ? href : undefined;
  } catch {
    return undefined;
  }
}
