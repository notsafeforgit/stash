import type React from "react";
import { useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "src/components/ui/button";
import { Checkbox } from "src/components/ui/checkbox";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";
import { Lightbox, type LightboxSlide } from "src/components/lightbox/lightbox";

// Pieces shared by every per-entity scrape merge dialog (performer, scene,
// future image/gallery). Kept thin: only the UI primitives that are
// genuinely identical across dialogs live here. Per-entity field projection
// and apply logic stays inside each dialog.

export type MergeMode = "merge" | "overwrite";

interface RowShellProps {
  label: string;
  accepted: boolean;
  onAcceptedChange: (next: boolean) => void;
  current: React.ReactNode;
  scraped: React.ReactNode;
  /** Optional merge/overwrite toggle for multi-value fields. When supplied,
   *  the toggle renders next to the field label. */
  mergeMode?: MergeMode;
  onMergeModeChange?: (next: MergeMode) => void;
}

export function RowShell({
  label,
  accepted,
  onAcceptedChange,
  current,
  scraped,
  mergeMode,
  onMergeModeChange,
}: RowShellProps) {
  const intl = useIntl();
  return (
    <div className="grid grid-cols-[24px_1fr_1fr] gap-3 items-start py-2 border-b border-border/50 last:border-b-0">
      <div className="pt-1">
        <Checkbox checked={accepted} onCheckedChange={onAcceptedChange} />
      </div>
      <div className="min-w-0">
        <div className="text-xs font-semibold text-muted-foreground mb-0.5 flex items-center gap-2 flex-wrap">
          <span>{label}</span>
          {mergeMode && onMergeModeChange && (
            <ToggleGroup<MergeMode>
              value={[mergeMode]}
              onValueChange={(vals) => {
                const next = vals[0];
                if (next) onMergeModeChange(next);
              }}
              variant="outline"
              size="sm"
              aria-label={intl.formatMessage({
                id: "scrape.merge_mode",
                defaultMessage: "Merge mode",
              })}
            >
              <ToggleGroupItem<MergeMode> value="merge">
                {intl.formatMessage({
                  id: "scrape.merge_mode_merge",
                  defaultMessage: "Merge",
                })}
              </ToggleGroupItem>
              <ToggleGroupItem<MergeMode> value="overwrite">
                {intl.formatMessage({
                  id: "scrape.merge_mode_overwrite",
                  defaultMessage: "Overwrite",
                })}
              </ToggleGroupItem>
            </ToggleGroup>
          )}
        </div>
        <div className="text-sm break-words text-muted-foreground/80">
          {current}
        </div>
      </div>
      <div className="min-w-0 text-sm break-words pt-4">{scraped}</div>
    </div>
  );
}

export function emptyOrText(value: string | null | undefined) {
  if (value == null || value === "") return <span className="italic">—</span>;
  return value;
}

/**
 * The merge-mode toggle as a standalone label affix — used for non-RowShell
 * groupings (the tags/performers/groups sections in the scene dialog) where
 * the rows live below the section header rather than inside one row.
 */
interface SectionHeaderProps {
  label: string;
  mergeMode?: MergeMode;
  onMergeModeChange?: (next: MergeMode) => void;
}

export function SectionHeader({
  label,
  mergeMode,
  onMergeModeChange,
}: SectionHeaderProps) {
  const intl = useIntl();
  return (
    <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground py-1 flex items-center gap-2 flex-wrap">
      <span>{label}</span>
      {mergeMode && onMergeModeChange && (
        <ToggleGroup<MergeMode>
          value={[mergeMode]}
          onValueChange={(vals) => {
            const next = vals[0];
            if (next) onMergeModeChange(next);
          }}
          variant="outline"
          size="sm"
          aria-label={intl.formatMessage({
            id: "scrape.merge_mode",
            defaultMessage: "Merge mode",
          })}
        >
          <ToggleGroupItem<MergeMode> value="merge">
            {intl.formatMessage({
              id: "scrape.merge_mode_merge",
              defaultMessage: "Merge",
            })}
          </ToggleGroupItem>
          <ToggleGroupItem<MergeMode> value="overwrite">
            {intl.formatMessage({
              id: "scrape.merge_mode_overwrite",
              defaultMessage: "Overwrite",
            })}
          </ToggleGroupItem>
        </ToggleGroup>
      )}
    </div>
  );
}

// ── Image picker (current + scraped carousel + lightbox) ──────────────────────

interface ImagePickerCellsProps {
  currentImage: string | null;
  scrapedImages: string[];
  selectedIndex: number;
  setSelectedIndex: (n: number) => void;
  /** Notifies the parent dialog so it can swallow its own Escape close while
   *  the lightbox is open (otherwise both Dialog + Lightbox handle the
   *  document-level keydown and the merge dialog dismisses too). */
  onLightboxOpenChange?: (open: boolean) => void;
}

type LightboxState = {
  open: boolean;
  slides: string[];
  /** "scraped" while navigating over the scraped image carousel — that's the
   *  one whose final index we mirror back to selectedIndex. "current" is the
   *  single-image preview of the form's existing image. */
  source: "scraped" | "current";
  index: number;
};

/**
 * Returns the `current` and `scraped` cells for the image row. The scraped
 * cell exposes prev/next + counter when there is more than one scraped image
 * and clicks open the shared Lightbox so the user can compare full-size.
 */
export function useImagePickerCells({
  currentImage,
  scrapedImages,
  selectedIndex,
  setSelectedIndex,
  onLightboxOpenChange,
}: ImagePickerCellsProps): {
  current: React.ReactNode;
  scraped: React.ReactNode;
  lightbox: React.ReactNode;
} {
  const intl = useIntl();
  const [lightbox, setLightbox] = useState<LightboxState>({
    open: false,
    slides: [],
    source: "scraped",
    index: 0,
  });
  const safeIndex = Math.max(
    0,
    Math.min(selectedIndex, scrapedImages.length - 1),
  );
  const selected = scrapedImages[safeIndex];

  function openLightbox(
    slides: string[],
    index: number,
    source: LightboxState["source"],
  ) {
    setLightbox({ open: true, slides, source, index });
    onLightboxOpenChange?.(true);
  }

  function closeLightbox() {
    // When the user navigated over the scraped carousel inside the lightbox,
    // mirror their final index back to the row so the thumbnail + apply
    // pick up the new selection.
    if (lightbox.source === "scraped") setSelectedIndex(lightbox.index);
    setLightbox((s) => ({ ...s, open: false }));
    onLightboxOpenChange?.(false);
  }

  const viewImageLabel = intl.formatMessage({
    id: "actions.view_image",
    defaultMessage: "View image",
  });
  // Override Button defaults (border, padding, gap) so the image fills the
  // entire 80px square — Tailwind merge resolves the conflicts.
  const thumbBtnClasses =
    "size-20 rounded-sm overflow-hidden p-0 border-0 bg-transparent hover:bg-transparent";

  const current = currentImage ? (
    <Button
      type="button"
      variant="ghost"
      onClick={() => openLightbox([currentImage], 0, "current")}
      className={thumbBtnClasses}
      aria-label={viewImageLabel}
    >
      <img
        src={currentImage}
        alt=""
        className="size-20 object-cover bg-muted"
      />
    </Button>
  ) : (
    emptyOrText(null)
  );

  const scraped = (
    <div className="flex flex-col gap-1">
      <Button
        type="button"
        variant="ghost"
        onClick={() => openLightbox(scrapedImages, safeIndex, "scraped")}
        className={thumbBtnClasses}
        aria-label={viewImageLabel}
      >
        <img src={selected} alt="" className="size-20 object-cover bg-muted" />
      </Button>
      {scrapedImages.length > 1 && (
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() =>
              setSelectedIndex(
                (safeIndex - 1 + scrapedImages.length) % scrapedImages.length,
              )
            }
            aria-label={intl.formatMessage({
              id: "actions.previous",
              defaultMessage: "Previous",
            })}
          >
            <ChevronLeft className="size-4" />
          </Button>
          <span className="text-xs tabular-nums text-muted-foreground select-none">
            {safeIndex + 1} / {scrapedImages.length}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() =>
              setSelectedIndex((safeIndex + 1) % scrapedImages.length)
            }
            aria-label={intl.formatMessage({
              id: "actions.next",
              defaultMessage: "Next",
            })}
          >
            <ChevronRight className="size-4" />
          </Button>
        </div>
      )}
    </div>
  );

  // Memoize the LightboxSlide objects so YARL receives a stable `slides`
  // array reference across navigation re-renders. Without this, every
  // index change rebuilt the array and the Thumbnails plugin re-initialized
  // its positioning, producing a visible jump when looping past the edges.
  const lightboxSlides = useMemo<LightboxSlide[]>(
    () => lightbox.slides.map((src) => ({ src })),
    [lightbox.slides],
  );

  const lightboxNode = (
    <Lightbox
      open={lightbox.open}
      onClose={closeLightbox}
      slides={lightboxSlides}
      index={lightbox.index}
      onView={(i) => setLightbox((s) => ({ ...s, index: i }))}
    />
  );

  return { current, scraped, lightbox: lightboxNode };
}

interface ImageMergeRowProps {
  accepted: boolean;
  onAcceptedChange: (v: boolean) => void;
  currentImage: string | null;
  scrapedImages: string[];
  selectedIndex: number;
  setSelectedIndex: (n: number) => void;
  onLightboxOpenChange?: (open: boolean) => void;
  label?: string;
}

export function ImageMergeRow({
  accepted,
  onAcceptedChange,
  currentImage,
  scrapedImages,
  selectedIndex,
  setSelectedIndex,
  onLightboxOpenChange,
  label = "Image",
}: ImageMergeRowProps) {
  const { current, scraped, lightbox } = useImagePickerCells({
    currentImage,
    scrapedImages,
    selectedIndex,
    setSelectedIndex,
    onLightboxOpenChange,
  });
  return (
    <>
      <RowShell
        label={label}
        accepted={accepted}
        onAcceptedChange={onAcceptedChange}
        current={current}
        scraped={scraped}
      />
      {lightbox}
    </>
  );
}
