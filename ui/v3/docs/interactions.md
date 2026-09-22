# Interaction and layout contracts

This guide owns the detailed motion, focus, overlay, and mobile layout contracts
for v3. Start with the [frontend architecture](architecture.md) for module and
state ownership, and retain its [interaction and accessibility policy](architecture.md#interaction-and-accessibility)
when changing these surfaces. Player source/seek behavior belongs in the
[player guide](player.md).

Paths below are relative to `src/`. The main entry points are
[route-viewport.tsx](../src/components/layout/route-viewport.tsx),
[mobile-detail-chrome.tsx](../src/components/layout/mobile-detail-chrome.tsx),
[mobile-search-row.tsx](../src/components/layout/mobile-search-row.tsx), and
[entity-edit-sheet.tsx](../src/components/detail/entity-edit-sheet.tsx).

## Motion and visual lifetime

`core/motion.ts` defines the shared motion timings. `core/content-reveal.ts`
owns interruptible Web Animations on the empty surface in
`layout/content-reveal.tsx`; it never animates the image/player subtree.
`core/route-transitions.ts` uses this for the 200ms committed-page reveal in
`layout/route-viewport.tsx`.
The viewport prepares the 12% cover in its layout effect before the destination's
first paint, then fades it to transparent. Starting from `onResolved` could dim
an already visible destination. The transparent final frame is held through
cleanup so the inline starting opacity cannot flash at animation completion.
Browser Back/Forward traversal gets no extra reveal: Safari already animates its
restored swipe snapshot. App navigation, including Smart Back, still reveals.
The image/video/scroller subtree stays opaque and untransformed. It does not
take snapshots or wait before committing navigation.
WebKit profiling with real Home thumbnails found native snapshot capture could
add 0.6–1.1 seconds. A separate paint surface also avoids promoting that whole
subtree during rapid navigation. Shell controls and portaled overlays stay
still. No keyed wrappers or forced remounts are involved.
Search/filter/hash changes and initial load do not reveal the whole page.
Reduced Motion skips the reveals and cancels a running one.
Smart Back supplies typed `state.navigationDirection: "back"`; exceptional
navigations can set `state.routeMotion: false`. Rapid navigation cancels the
previous animation, as does a visibility or Reduce Motion change. A pending
reveal belongs to its destination and is consumed once by the viewport commit.
Finished effects are canceled and their paint surface is hidden. Browsers without Web
Animations navigate normally. `layout/mobile-navigation.tsx` owns one persistent
navigation drawer for all toolbars. Its lease holds only the visual reveal until
the drawer exits; navigation and loading continue immediately. This drawer uses
a dimmed backdrop to avoid filtering a changing image-heavy page.
Local view changes use the same empty surface with a 140ms reveal. `EntityList`
observes layout mode, zoom, aspect ratio, mobile columns and pagination; its
surface sits outside the list scroller. Detail layouts reveal only the selected
tab panel or the focused/inline viewer area, preserving visited panels, media,
focus and scroll state. Background data refreshes and selection changes do not
restart motion. Surfaces are capped at a viewport's height, and only an active
list can animate. Grid zoom commits immediately on all devices; it no longer
captures native View Transition snapshots.

Image and scene lightboxes share `lightbox/use-lightbox-motion.ts`: a 240ms
entrance reveal, 180ms exit, 240ms swipe settling and 180ms button/keyboard
navigation. Image zoom keeps its 250ms timing. An empty black surface in YARL's
controls slot fades away to reveal the media and fades back for dismissal.
The media stays opaque and untransformed, preserving video and swipe geometry
and avoiding dropped frames from compositing the whole lightbox in WebKit at
phone pixel densities. `core/paint-animation.ts` prepares a temporary layer
and holds the first frame through a paint before starting the clock, so startup
work cannot consume the entrance unseen. The cover starts at 99% opacity to
allow initial media rasterization. YARL's root CSS opacity transition is disabled
when Web Animations are available; unsupported browsers retain the native fade.
YARL owns gestures and exit completion.
Mobile Close, Escape and browser Back use the library's exit before
disposing the player, consuming exactly one history entry. The optional visual
dismissal callback in `use-lightbox-history.ts` also preserves the existing
history-only contract for the focused scene viewer.

`cards/use-card-press.ts` uses the same interruptible animation owner to enlarge
the entire card slightly on primary-pointer down and ease it back on release.
It does not rerender the card or delay navigation/playback. Scrolling, pointer
cancellation, context menus, selection and nested controls cancel or bypass
this feedback. Neither path captures native View Transition snapshots. Effects,
pending frames and temporary listeners are released on completion, unmount,
visibility changes or Reduce Motion changes.

## Dialog dismissal

Form and confirmation dialogs have one visible dismissal action: Cancel beside
Save, Create, Apply, or the destructive action. Set `showCloseButton={false}`
when supplying that action; do not add a second corner X or Close footer.
Informational and immediately applied controls keep one Close action, while
search dialogs without footer actions retain their labelled corner close.
Escape, backdrop dismissal, focus management, and busy-state guards remain
owned by the dialog primitive and its caller.
Use `DialogContent.initialFocus` for a specific initial field instead of the
input's `autoFocus`, so the dialog can capture and restore the previous focus.

TV's `TvDialogContent` uses `variant="form"` when its child owns the scrolling
fields and pinned actions. Its default content variant provides a bounded
scroller and one Close footer. `MarkerEditForm`'s dialog layout requires an
`onCancel` handler and keeps Cancel/Save visible on small screens; its inline
layout also offers Discard to reset fields without leaving the editor.

## Entity editing

All seven single-entity edit sheets use `components/detail/entity-edit-sheet.tsx`.
Its fixed header provides the title. Close stays in the bottom action area on
mobile and in the header on desktop, including while data is loading or
unavailable. Inline detail editors use `detail-editor-layout.tsx` with the same
placement. Forms own their scrolling fields and
pinned action bars inside the remaining height. Close, Escape, and backdrop
dismissal leave without saving; Discard resets the form and keeps the pane open.
Successful saves close the sheet through the existing form callback.

## Settings navigation

`SettingsLayout` keeps the settings page mounted in its own scroller. Desktop
uses a sidebar with section links and inline search results. Mobile reserves one
56px bottom row for Navigation, the current section, and Search, replacing the
global bottom navigation bar. Its top header contains only the Settings title.
Section links open in an upward menu with 44px targets and bounded scrolling.
They remain TanStack Router links, so deep links and browser Back select the
correct section.

Search replaces the mobile row and focuses inside the opening touch handler.
It reuses the generated, locale-resolved settings index and navigates with the
existing `hl` parameter to reveal the chosen setting. Both layouts render results
as ordinary route links with native touch and keyboard activation. Mobile results
appear above the input, bounded to half the visible viewport, while the shared
keyboard layout hook reserves space below the footer. Closing search
restores its trigger's focus. Search state belongs to the navigation, so a
breakpoint change preserves the query without remounting the settings form.

## Mobile detail navigation

Collection and media detail layouts keep the entity title above the scroller
and navigation below it on mobile. `mobile-detail-chrome.tsx` provides a shared
56px toolbar with direct Navigation, section picker, Search, Filters, View options,
Entity actions, and Back controls. All targets stay at least 44px at 320px width;
the section label truncates and becomes an icon on the narrowest screens. Search
and selection replace that row, with their dismissal control at its right edge. Navigation,
Filters, View options, and Entity actions open bottom drawers with scrollable
content and dismiss through swipe-down, outside taps, or Escape. They omit Close
rows; the shared drawer primitive supplies bottom safe-area padding. The section
picker includes page navigation and a page-jump form. Previous/next controls also
appear at the end of list results, and single-page lists omit pagination. Standalone mobile lists use the same
row modes with navigation and a page picker. Desktop retains
its sidebar controls and tab strip. Collection pages use the `md` breakpoint;
media pages use `lg`, matching their existing split layouts.

An active list query keeps Search highlighted even when its input is closed.
Tap opens the input; long press opens the existing Base UI context menu anchored
above the Search button, independent of the finger's position within it, with the
full query and Edit search/Clear search actions. Clearing only changes the query
and resets pagination, preserving sort and other filter criteria. Filters and
View options also show the query beneath their drawer title, so its visibility
does not depend on discovering the long-press shortcut. Long queries wrap within
the popup or drawer width without widening the toolbar.

List and settings search share `MobileSearchRow` and `useMobileSearch`. The outer
chevron collapses search; the X inside the input clears its text. A short native
Web Animation reveals the row from the measured Search button bounds and folds
it back on close. Only clipping and opacity animate: the input keeps its final
layout size and position throughout, and focus still happens inside the opening
gesture. Blur flushes pending text before the exit animation; its completion
restores the toolbar and focus. Interrupted animations cancel cleanly, unmount
cancels pending work, and reduced-motion preference skips the animation.

Drawer motion uses `transform` for dragging and enter/exit animations. Do not
combine it with the separate CSS `translate` property on the popup: Base UI
supplies an inline transform while dragging, and the two translations add
together instead of tracking the pointer one-to-one.

The footer participates in flex layout, reserving its actual height without
fixed offsets or content overlays, including Home's bottom navigation. The shell
owns top/side safe-area insets; footers own the bottom inset. Shared CSS tokens
read Safari's current `env(safe-area-inset-*)` values in browser and Home Screen
modes. Toolbar margins grow from 6px at 320px to 16px on wider phones while
retaining every 44px touch target. Portaled drawers, sheets and full-screen player
controls account for their own screen edges. The keyboard replaces the bottom
safe-area padding rather than adding a second gap above its chrome. The shared
`useMobileKeyboardLayout` hook reserves the portion of the layout viewport below
the visual viewport as a bottom margin, shrinking the adjacent scroller so list
content ends at the search row. It accounts for Safari's native viewport pan and
updates a dedicated CSS variable synchronously on focus/viewport events, avoiding
an extra animation frame or React render between the pan and layout correction.
Do not translate only the footer: that leaves content under the keyboard chrome
and can briefly compound the browser's pan. Opening search mounts and focuses in
the same touch handler and permits native focus scrolling; restoring the trigger
on close uses `preventScroll`. This applies to standalone lists, detail footers,
and settings. Physical iPhone keyboard animation still requires device validation.
React portals move controls into the footer's typed slots while preserving
their tab, list, and action contexts. Only the active list publishes controls;
previously visited panels stay mounted with their filters and state intact.
The mobile picker uses the same Base UI tab state as desktop, with vertical
triggers in an upward popover, and brings a chosen section into view. The
section popover and action drawer keep their portal targets mounted so tabs
and action dialogs survive closing them. `entity-actions-menu.tsx` renders shared
typed action definitions as desktop dropdowns or direct mobile rows. Desktop
submenus become labelled, flat groups on mobile; invoking an action closes the
drawer before showing its form or confirmation. Search mounts and focuses
within the opening touch handler, and flushes any pending debounce on blur
before the row closes.
List controls own their prop contract; the parent bar extends it with view
settings. Page jumping uses TanStack Form with Zod validation and starts a new
draft when the page or page count changes. Collection and media tab panels both
publish their active state through `ListActivityContext`, so kept-mounted lists
cannot leave duplicate controls in the footer.
Tapping the current section also reveals it when the page is showing the
entity information or media above it. The focused scene viewer keeps its
existing player mounted and places Close below it on mobile.
Collection panels use their scroller's container height as a minimum so a
shorter list cannot clamp the viewport back into the entity information.

## Validation

Use the [browser validation guide](development.md#validation) for the Chromium
and WebKit fixtures. Check Back/Forward, interrupted navigation, reduced motion,
focus restoration, and dismissal while data or lazy code is pending. Changes to
mobile keyboard layout and playback gestures also need physical iPhone checks;
desktop WebKit fixtures do not establish those device behaviors.
