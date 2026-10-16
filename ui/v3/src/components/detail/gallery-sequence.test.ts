import {
  ApolloClient,
  ApolloLink,
  InMemoryCache,
  Observable,
} from "@apollo/client";
import { describe, expect, it } from "vitest";
import { GallerySequence } from "./gallery-sequence";
import { SortDirectionEnum } from "@/core/generated-graphql";

function fixture() {
  let changed = false;
  const reads: {
    name: string | undefined;
    variables: Record<string, unknown>;
  }[] = [];
  const query = {
    filter: {
      page: 2,
      per_page: 1,
      q: "chosen",
      sort: "date",
      direction: SortDirectionEnum.Desc,
    },
    gallery_filter_ast: {
      root: { condition: { field: "organized", value: true } },
    },
  };
  const client = new ApolloClient({
    cache: new InMemoryCache(),
    link: new ApolloLink(
      (operation) =>
        new Observable((observer) => {
          const { operationName: name, variables } = operation;
          reads.push({ name, variables });
          if (name === "FindGalleryLightboxList") {
            const id = variables.filter.page === 2 ? "long" : "short";
            observer.next({
              data: {
                findGalleries: { count: 2, galleries: [{ id, title: id }] },
              },
            });
          } else if (name === "FindGalleryImageID") {
            observer.next({
              data: { findGallery: { id: "long", image: { id: "long-60" } } },
            });
          } else {
            const count = variables.id === "long" ? 61 : 1;
            const offset = variables.offset;
            observer.next({
              data: {
                findGallery: {
                  id: variables.id,
                  media: {
                    count,
                    signature: changed && offset ? "changed" : variables.id,
                    next_offset: offset + 60 < count ? offset + 60 : null,
                    items: Array.from(
                      { length: Math.min(count - offset, 60) },
                      (_, index) => ({
                        source_post_uuid: null,
                        source_position: null,
                        scene: null,
                        image: {
                          id: `${variables.id}-${offset + index}`,
                          title: "Image",
                          paths: {
                            image: "/image.jpg",
                            preview: "",
                            thumbnail: "",
                          },
                          visual_files: [],
                        },
                      }),
                    ),
                  },
                },
              },
            });
          }
          observer.complete();
        }),
    ),
  });
  const start = {
    gallery: { id: "long", title: "Long gallery" },
    query,
    imageIndex: 0,
    autoplay: false,
  };
  return {
    client,
    start,
    reads,
    change: () => {
      changed = true;
    },
    reader: new GallerySequence(client, start, new AbortController().signal),
  };
}

describe("gallery continuation", () => {
  it("pages mixed media before moving galleries and lands on the previous gallery's last page", async () => {
    const { reader, reads, start } = fixture();
    const { page } = await reader.first();
    const tail = await reader.adjacent(page, "next");
    expect(tail.offset).toBe(60);
    expect(tail.media.items[0]?.image?.id).toBe("long-60");
    const short = await reader.adjacent(tail, "next");
    expect(short.gallery.id).toBe("short");
    const previous = await reader.adjacent(short, "previous");
    expect(previous.gallery.id).toBe("long");
    expect(previous.offset).toBe(60);
    expect((await reader.adjacent(previous, "previous")).offset).toBe(0);
    for (const read of reads.filter(
      (read) => read.name === "FindGalleryLightboxList",
    )) {
      expect(read.variables.gallery_filter_ast).toEqual(
        start.query.gallery_filter_ast,
      );
      expect(read.variables.filter).toMatchObject({
        q: "chosen",
        sort: "date",
        per_page: 1,
        direction: SortDirectionEnum.Desc,
      });
    }
  });

  it("rejects changed membership instead of joining unrelated pages", async () => {
    const { reader, change } = fixture();
    const { page } = await reader.first();
    change();
    await expect(reader.adjacent(page, "next")).rejects.toMatchObject({
      code: "album_changed",
    });
  });

  it("resolves a scrubbed image by identity rather than a mixed-media index", async () => {
    const { client, start } = fixture();
    const reader = new GallerySequence(
      client,
      { ...start, imageIndex: 12 },
      new AbortController().signal,
    );
    const { page, index } = await reader.first();
    expect(page.offset).toBe(60);
    expect(index).toBe(0);
    expect(page.media.items[index]?.image?.id).toBe("long-60");
  });
});
