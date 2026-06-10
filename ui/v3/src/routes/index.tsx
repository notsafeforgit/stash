import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
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
  SavedFilterCarouselRow,
} from "src/components/frontpage/recommendation-row";
import { FrontPageConfig } from "src/components/frontpage/front-page-config";
import { CardAspectContext } from "src/components/list/card-aspect-context";

// ── FrontPage ──────────────────────────────────────────────────────────────────

function FrontPage() {
  const intl = useIntl();
  const { configuration } = useConfigurationContext();
  const [configOpen, setConfigOpen] = useState(false);
  const [saving, setSaving] = useState(false);

  const [saveUISetting] = useConfigureUISetting();

  const ui = configuration.ui;
  const rows: FrontPageContent[] =
    getFrontPageContent(ui) ?? generateDefaultFrontPageContent(intl);

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
      <div className="flex flex-col gap-6 py-4 flex-1 min-h-0 overflow-y-auto">
        {rows.map((content, i) => {
          if (content.__typename === "SavedFilter") {
            return <SavedFilterCarouselRow key={i} content={content} />;
          }
          const heading =
            content.title ??
            (content.message
              ? intl.formatMessage(
                  {
                    id: content.message.id,
                    defaultMessage: content.message.id,
                  },
                  content.message.values,
                )
              : `${content.mode}`);
          return (
            <CustomFilterCarouselRow
              key={i}
              heading={heading}
              content={content}
            />
          );
        })}

        {/* Customise button */}
        <div className="flex justify-center pb-4">
          <Button
            variant="outline"
            disabled={saving}
            className="gap-2 text-muted-foreground hover:text-foreground"
            onClick={() => setConfigOpen(true)}
          >
            <Settings2 size={15} />
            {saving
              ? intl.formatMessage({ id: "saving", defaultMessage: "Saving…" })
              : intl.formatMessage({
                  id: "customise",
                  defaultMessage: "Customise",
                })}
          </Button>
        </div>

        <FrontPageConfig
          open={configOpen}
          rows={rows}
          onClose={() => setConfigOpen(false)}
          onSave={handleSave}
        />
      </div>
    </CardAspectContext.Provider>
  );
}

// ── Route ──────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/")({
  component: FrontPage,
});
