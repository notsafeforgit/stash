/** Short, interruptible transitions for navigation and local view changes. */
export const motion = {
  // Keep content readable throughout a reveal; an opaque cover flashes when
  // the router resolves after the destination's first paint.
  contentCoverOpacity: 0.12,
  duration: {
    page: 200,
    content: 140,
    lightbox: 180,
    lightboxEnter: 240,
    press: 100,
    release: 180,
    swipe: 240,
    zoom: 250,
  },
  easing: {
    reveal: "ease-out",
    fade: "cubic-bezier(0.2, 0, 0, 1)",
  },
} as const;
