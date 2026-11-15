/**
 * The slide an automatic advance lands on after `slide`, or undefined when
 * there is nowhere to go. Mirrors the carousel's `next()`: a finite carousel
 * stops at its last slide, a looping one wraps, and a lone slide never
 * advances onto itself.
 */
export function followingSlide<T>(
  slides: readonly T[],
  slide: T,
  finite: boolean,
): T | undefined {
  const index = slides.indexOf(slide);
  if (index < 0) return undefined;
  if (finite) return slides[index + 1];
  const next = slides[(index + 1) % slides.length];
  return next === slide ? undefined : next;
}
