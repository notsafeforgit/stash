import type { ApolloClient } from "@apollo/client";
import {
  FindGalleryLightboxListDocument,
  FindGalleryMediaDocument,
  FindGalleryImageIdDocument,
  type FindGalleriesQueryVariables,
} from "@/core/generated-graphql";
import { NativeArchiveError } from "@/core/native-archive/client";
import type { GalleryMediaPage } from "./gallery-media";

export interface GallerySequenceStart {
  gallery: { id: string; title?: string | null };
  query: FindGalleriesQueryVariables;
  imageIndex: number;
  autoplay: boolean;
}
export type GallerySequencePage = {
  gallery: GallerySequenceStart["gallery"];
  listPage: number;
  listIndex: number;
  offset: number;
  media: GalleryMediaPage;
};
export type GalleryDirection = "next" | "previous";
const mediaPageSize = 60;

/** One viewer owns its requests and bounded caches; closing aborts everything.
 * The list query is captured on open, so paging never changes the grid URL. */
export class GallerySequence {
  private lists = new Map<
    number,
    Promise<{
      count: number;
      galleries: { id: string; title?: string | null }[];
    }>
  >();
  private media = new Map<string, Promise<GalleryMediaPage>>();
  private signatures = new Map<string, string>();
  private readonly context;

  constructor(
    private client: ApolloClient,
    private start: GallerySequenceStart,
    signal: AbortSignal,
  ) {
    this.context = { queryDeduplication: false, fetchOptions: { signal } };
  }

  private cached<K, V>(
    cache: Map<K, Promise<V>>,
    key: K,
    load: () => Promise<V>,
  ) {
    const existing = cache.get(key);
    if (existing) return existing;
    const value = load().catch((error: unknown) => {
      cache.delete(key);
      throw error;
    });
    cache.set(key, value);
    if (cache.size > 4) cache.delete(cache.keys().next().value!);
    return value;
  }

  private list(page: number) {
    return this.cached(this.lists, page, async () => {
      const { data } = await this.client.query({
        query: FindGalleryLightboxListDocument,
        variables: {
          ...this.start.query,
          filter: { ...this.start.query.filter, page },
        },
        fetchPolicy: "no-cache",
        context: this.context,
      });
      if (!data) throw new Error("Gallery list unavailable");
      return data.findGalleries;
    });
  }

  private read(id: string, offset: number) {
    return this.cached(this.media, `${id}:${offset}`, async () => {
      const { data } = await this.client.query({
        query: FindGalleryMediaDocument,
        variables: { id, offset, limit: mediaPageSize },
        fetchPolicy: "no-cache",
        context: this.context,
      });
      const media = data?.findGallery?.media;
      if (!media) throw new Error("Gallery media unavailable");
      const signature = this.signatures.get(id);
      if (signature !== undefined && signature !== media.signature)
        throw new NativeArchiveError(409, "album_changed");
      this.signatures.set(id, media.signature);
      if (this.signatures.size > 8)
        this.signatures.delete(this.signatures.keys().next().value!);
      return media;
    });
  }

  async first(): Promise<{ page: GallerySequencePage; index: number }> {
    const listPage = this.start.query.filter?.page ?? 1;
    const [list, media] = await Promise.all([
      this.list(listPage),
      this.read(this.start.gallery.id, 0),
    ]);
    const listIndex = list.galleries.findIndex(
      (g) => g.id === this.start.gallery.id,
    );
    if (listIndex < 0) throw new NativeArchiveError(409, "album_changed");
    let page = {
      gallery: this.start.gallery,
      listPage,
      listIndex,
      offset: 0,
      media,
    };
    if (!media.items.length)
      return { page: await this.adjacent(page, "next"), index: 0 };
    // A desktop scrubber indexes the gallery's image-only preview order.
    // Resolve that actual image instead of treating it as a mixed-media offset.
    if (this.start.imageIndex > 0) {
      const { data } = await this.client.query({
        query: FindGalleryImageIdDocument,
        variables: { id: page.gallery.id, index: this.start.imageIndex },
        fetchPolicy: "no-cache",
        context: this.context,
      });
      const imageId = data?.findGallery?.image?.id;
      while (imageId) {
        const index = page.media.items.findIndex(
          (item) => item.image?.id === imageId,
        );
        if (index >= 0) return { page, index };
        if (page.media.next_offset == null) break;
        page = await this.at(page, page.media.next_offset);
      }
    }
    return { page, index: 0 };
  }

  private async at(
    page: GallerySequencePage,
    offset: number,
  ): Promise<GallerySequencePage> {
    return { ...page, offset, media: await this.read(page.gallery.id, offset) };
  }

  async adjacent(
    page: GallerySequencePage,
    direction: GalleryDirection,
  ): Promise<GallerySequencePage> {
    if (direction === "next" && page.media.next_offset != null)
      return this.at(page, page.media.next_offset);
    if (direction === "previous" && page.offset > 0)
      return this.at(page, Math.max(0, page.offset - mediaPageSize));

    let listPage = page.listPage;
    let listIndex = page.listIndex;
    let list = await this.list(listPage);
    const count = list.count;
    const perPage = this.start.query.filter?.per_page ?? 40;
    const pages = perPage < 1 ? 1 : Math.max(1, Math.ceil(count / perPage));
    // Empty galleries are skipped in both directions, including across pages.
    // Bound a full traversal so an entirely empty result cannot spin forever.
    for (let visited = 0; visited < count; visited++) {
      listIndex += direction === "next" ? 1 : -1;
      if (listIndex >= list.galleries.length || listIndex < 0) {
        listPage =
          direction === "next"
            ? (listPage % pages) + 1
            : ((listPage + pages - 2) % pages) + 1;
        list = await this.list(listPage);
        listIndex = direction === "next" ? 0 : list.galleries.length - 1;
      }
      const gallery = list.galleries[listIndex];
      if (!gallery) continue;
      const media = await this.read(gallery.id, 0);
      if (!media.items.length) continue;
      const next = { gallery, listPage, listIndex, offset: 0, media };
      if (direction === "previous" && media.count > mediaPageSize)
        return this.at(
          next,
          Math.floor((media.count - 1) / mediaPageSize) * mediaPageSize,
        );
      return next;
    }
    return page;
  }
}
