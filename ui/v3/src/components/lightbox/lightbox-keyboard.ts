const POPUP_ROLES = '[role="menu"], [role="alertdialog"], [role="listbox"]';

/** Touch-opened menus may leave focus on body in Safari. Their open
 * state, not just focus, must keep the lightbox from consuming Escape/arrows.
 * Tooltips are intentionally excluded: they don't own viewer navigation. */
export function popupOwnsLightboxKeys() {
  return !!(
    document.activeElement?.closest(`${POPUP_ROLES}, [role="combobox"]`) ||
    document.querySelector(`:is(${POPUP_ROLES})[data-open]`)
  );
}
