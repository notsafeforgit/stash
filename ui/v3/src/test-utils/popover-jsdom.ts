/**
 * jsdom has no popover API, layout or hit testing. These stubs let ui-sdk
 * popups open and close in unit tests; positions are not measured.
 */
export function installPopoverStubs() {
  HTMLElement.prototype.showPopover = function showPopover() {};
  HTMLElement.prototype.hidePopover = function hidePopover() {};
  Element.prototype.getAnimations = () => [];
  Element.prototype.scrollIntoView = function scrollIntoView() {};
  Element.prototype.checkVisibility = () => true;
  const noRects: DOMRectList = {
    length: 0,
    item: () => null,
    [Symbol.iterator]: () => [].values(),
  };
  Element.prototype.getClientRects = () => noRects;
  document.elementFromPoint = () => null;
  document.elementsFromPoint = () => [];
  class StubResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver ??= StubResizeObserver;
  if (!document.head.querySelector("style[data-popover-stub]")) {
    const style = document.createElement("style");
    style.dataset.popoverStub = "";
    style.textContent = "[popover] { display: block !important }";
    document.head.append(style);
  }
}

/**
 * Call after unmounting. When the focused element is removed, jsdom keeps the
 * document as its focus pointer, so the next focus() dispatches a window blur
 * that closes a newly opened menu. Focusing and blurring a connected probe
 * clears that pointer. Browsers do not have this stale state.
 */
export function clearStaleFocus() {
  const probe = document.createElement("button");
  document.body.append(probe);
  probe.focus();
  probe.blur();
  probe.remove();
}
