import {
  lazy,
  Suspense,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useIntl } from "react-intl";
import { useDocumentTitle } from "src/hooks/title";
import { Settings2 } from "lucide-react";
import { Button } from "src/components/ui/button";
import { useConfigurationContext } from "src/hooks/config";
import { useConfigureUISetting } from "src/hooks/config";
import {
  getFrontPageContent,
  generateDefaultFrontPageContent,
  type FrontPageContent,
} from "src/core/config";
import {
  CustomFilterCarouselRow,
  RecommendationRow,
  SavedFilterCarouselRow,
} from "src/components/frontpage/recommendation-row";
import { CardAspectContext } from "src/components/list/card-aspect-context";
import { DeferredMount } from "@/components/shared/deferred-mount";
import { Spinner } from "@/components/ui/spinner";
import { FrontPageRowContext, useFrontPageState } from "./front-page-state";
import { formatFilterModeLabel } from "@/models/list-filter/labels";

const FrontPageConfig = lazy(() =>
  import("./front-page-config").then((module) => ({
    default: module.FrontPageConfig,
  })),
);

// ── FrontPage ──────────────────────────────────────────────────────────────────

export function FrontPage() {
  const intl = useIntl();
  // Home: just the app title (no page prefix).
  useDocumentTitle();
  const { configuration } = useConfigurationContext();
  const [configOpen, setConfigOpen] = useState(false);
  const [configMounted, setConfigMounted] = useState(false);
  const [saving, setSaving] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const [saveUISetting] = useConfigureUISetting();

  const ui = configuration.ui;
  const rows = useMemo(
    () => getFrontPageContent(ui) ?? generateDefaultFrontPageContent(intl),
    [ui, intl],
  );
  const visit = useFrontPageState(
    JSON.stringify([intl.locale, rows]),
    rows.length,
  );
  useLayoutEffect(() => {
    if (scrollRef.current) scrollRef.current.scrollTop = visit.scrollTop;
  }, [visit]);

  async function handleSave(updated: FrontPageContent[]) {
    setConfigOpen(false);
    setSaving(true);
    try {
      await saveUISetting({
        variables: { key: "frontPageContent", value: updated },
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    // FrontPage owns its own scroll container — `<main>` is overflow-hidden
    // so list pages can host an internal virtualised scroller; this page has
    // nothing equivalent and would otherwise clip its content.
    //
    // Wrap every row in `CardAspectContext.Provider value="portrait"` so the
    // homepage carousels all share one portrait aspect — scenes / images /
    // markers letterbox-into-portrait, performers / studios / groups / tags
    // already prefer portrait. The gallery row (whose covers are inherently
    // landscape page-spreads) resets the context back to `auto` for itself
    // — see `GalleryCarouselRow` in `recommendation-row.tsx`.
    <CardAspectContext.Provider value="portrait">
      <div
        ref={scrollRef}
        data-front-page
        onScroll={(event) => {
          visit.scrollTop = event.currentTarget.scrollTop;
        }}
        className="flex flex-col gap-6 py-4 flex-1 min-h-0 overflow-y-auto"
      >
        {rows.map((content, i) => {
          const heading =
            content.__typename === "SavedFilter"
              ? intl.formatMessage({ id: "saved_filter" })
              : (content.title ??
                (content.message
                  ? intl.formatMessage(
                      {
                        id: content.message.id,
                        defaultMessage: formatFilterModeLabel(
                          intl,
                          content.mode,
                        ),
                      },
                      content.message.values,
                    )
                  : formatFilterModeLabel(intl, content.mode)));
          return (
            <FrontPageRowContext.Provider
              key={`${visit.key}:${i}`}
              value={visit.rows[i]}
            >
              <DeferredMount
                eager={i === 0 || visit.rows[i]?.mounted}
                scrollRoot={scrollRef}
                fallback={
                  content.__typename === "SavedFilter" ? (
                    <SavedFilterCarouselRow content={content} placeholderOnly />
                  ) : (
                    <RecommendationRow
                      heading={heading}
                      mode={content.mode}
                      loading
                    >
                      {null}
                    </RecommendationRow>
                  )
                }
              >
                {content.__typename === "SavedFilter" ? (
                  <SavedFilterCarouselRow content={content} />
                ) : (
                  <CustomFilterCarouselRow
                    heading={heading}
                    content={content}
                  />
                )}
              </DeferredMount>
            </FrontPageRowContext.Provider>
          );
        })}

        {/* Customise button */}
        <div className="flex justify-center pb-4">
          <Button
            variant="outline"
            disabled={saving}
            className="gap-2 text-muted-foreground hover:text-foreground"
            onClick={() => {
              setConfigMounted(true);
              setConfigOpen(true);
            }}
          >
            <Settings2 data-icon="inline-start" />
            {saving
              ? intl.formatMessage({ id: "saving", defaultMessage: "Saving…" })
              : intl.formatMessage({
                  id: "customise",
                  defaultMessage: "Customise",
                })}
          </Button>
        </div>

        {configMounted && (
          <Suspense fallback={<Spinner className="mx-auto" />}>
            <FrontPageConfig
              open={configOpen}
              rows={rows}
              onClose={() => setConfigOpen(false)}
              onSave={handleSave}
            />
          </Suspense>
        )}
      </div>
    </CardAspectContext.Provider>
  );
}
