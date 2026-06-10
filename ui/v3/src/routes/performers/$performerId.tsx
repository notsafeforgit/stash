import React, { useState, useCallback } from "react";
import { cn } from "src/lib/utils";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery, useMutation } from "@apollo/client/react";
import { zodValidator } from "@tanstack/zod-adapter";
import { z } from "zod";
import { useIntl } from "react-intl";
import {
  DetailBackBar,
  DetailSidebarBack,
  DetailPageState,
} from "src/components/detail/detail-page-parts";
import { DetailTabs } from "src/components/detail/detail-tabs";
import { Skeleton } from "src/components/ui/skeleton";
import { Button } from "src/components/ui/button";
import { Star, Heart, Droplets, User, Pencil, ChevronLeft } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { PerformerDetailsTab } from "src/components/detail/performer-detail-tabs";
import { PerformerEditForm } from "src/components/detail/performer-edit-form";
import { PerformerActionsMenu } from "src/components/detail/performer-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import {
  PerformerScenesTab,
  PerformerImagesTab,
  PerformerGalleriesTab,
  PerformerGroupsTab,
} from "src/components/detail/performer-list-tabs";
import { Lightbox } from "src/components/lightbox";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  /** Active entity list tab (scenes / images / galleries / groups) */
  tab: z.string().optional(),
});

// ── Performer profile image ────────────────────────────────────────────────────

type PerformerData = NonNullable<GQL.FindPerformerQuery["findPerformer"]>;

// Mobile width policy for entity detail images: portrait images get
// clamped to 3/5 width and centered so they don't dominate the viewport;
// landscape / square images take the full pane width since they're shorter
// at the same width. Aspect is detected from the image's natural
// dimensions on load — fallback / loading states default to the entity's
// canonical aspect (`aspect-[2/3]` for performers / groups, `aspect-square`
// for tags / studios / galleries) so the placeholder stays compact.
function entityImageWidthClass(isPortrait: boolean) {
  return isPortrait ? "max-md:w-3/5 max-md:self-center md:w-full" : "w-full";
}

function PerformerPortrait({
  performer,
  onImageClick,
  imageUpdating,
}: {
  performer: PerformerData;
  onImageClick?: () => void;
  imageUpdating?: boolean;
}) {
  const [failed, setFailed] = React.useState(false);
  const [isPortrait, setIsPortrait] = React.useState(true);

  if (imageUpdating) {
    return (
      <Skeleton className="max-md:w-3/5 max-md:self-center md:w-full shrink-0 aspect-[2/3] rounded" />
    );
  }

  if (!performer.image_path || failed) {
    return (
      <div className="max-md:w-3/5 max-md:self-center md:w-full shrink-0 aspect-[2/3] flex items-center justify-center bg-muted rounded text-muted-foreground">
        <User size={40} />
      </div>
    );
  }
  return (
    <Button
      variant="ghost"
      className={cn(
        entityImageWidthClass(isPortrait),
        "shrink-0 overflow-hidden rounded p-0 h-auto cursor-zoom-in hover:bg-transparent",
      )}
      onClick={onImageClick}
      aria-label="View full image"
    >
      <img
        src={performer.image_path}
        alt={performer.name}
        className="w-full h-auto"
        onLoad={(e) => {
          const img = e.currentTarget;
          setIsPortrait(img.naturalHeight > img.naturalWidth);
        }}
        onError={() => setFailed(true)}
      />
    </Button>
  );
}

// ── Performer toolbar ──────────────────────────────────────────────────────────

function PerformerToolbar({
  performer,
  onEdit,
  onToggleFavorite,
  onDeleted,
}: {
  performer: PerformerData;
  onEdit: () => void;
  onToggleFavorite: () => void;
  onDeleted: () => void;
}) {
  const intl = useIntl();

  return (
    <div className="flex flex-wrap gap-2">
      <Button
        variant="outline"
        size="sm"
        onClick={onToggleFavorite}
        className={cn(
          performer.favorite &&
            "text-rose-500 border-rose-400/60 hover:text-rose-400",
        )}
        title={intl.formatMessage({
          id: "favourite",
          defaultMessage: "Favourite",
        })}
      >
        <Heart size={13} className={performer.favorite ? "fill-current" : ""} />
      </Button>
      {performer.o_counter != null && (
        <span
          className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1 select-none"
          title={intl.formatMessage({
            id: "o_counter",
            defaultMessage: "O-Counter",
          })}
        >
          <Droplets size={13} />
          {performer.o_counter}
        </span>
      )}
      {performer.rating100 != null && (
        <span
          className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1 select-none"
          title={intl.formatMessage({
            id: "rating",
            defaultMessage: "Rating",
          })}
        >
          <Star size={13} />
          {performer.rating100}
        </span>
      )}
      <Button variant="outline" size="sm" onClick={onEdit}>
        <Pencil size={13} />
        {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
      </Button>
      <PerformerActionsMenu performer={performer} onDeleted={onDeleted} />
    </div>
  );
}

// ── Performer detail page ─────────────────────────────────────────────────────

function PerformerDetailPage() {
  const { performerId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/performers");
  const intl = useIntl();
  const [editOpen, setEditOpen] = useState(false);
  const [portraitLightboxOpen, setPortraitLightboxOpen] = useState(false);
  const [imageUpdating, setImageUpdating] = useState(false);
  const handleImageUpdateChange = useCallback(
    (updating: boolean) => setImageUpdating(updating),
    [],
  );

  const { data, loading, error } = useQuery(GQL.FindPerformerDocument, {
    variables: { id: performerId },
    fetchPolicy: "cache-first",
  });

  const [updatePerformer] = useMutation(GQL.PerformerUpdateDocument);

  const performer = data?.findPerformer;

  function handleToggleFavorite() {
    if (!performer) return;
    updatePerformer({
      variables: { input: { id: performer.id, favorite: !performer.favorite } },
      optimisticResponse: {
        performerUpdate: { ...performer, favorite: !performer.favorite },
      },
    });
  }

  // Build entity tabs (only tabs that have content)
  type EntityTab = { id: string; label: string; content: React.ReactNode };
  const entityTabs: EntityTab[] = performer
    ? [
        ...(performer.scene_count > 0
          ? [
              {
                id: "scenes",
                label: intl.formatMessage({
                  id: "scenes",
                  defaultMessage: "Scenes",
                }),
                content: <PerformerScenesTab performer={performer} />,
              },
            ]
          : []),
        ...(performer.image_count > 0
          ? [
              {
                id: "images",
                label: intl.formatMessage({
                  id: "images",
                  defaultMessage: "Images",
                }),
                content: (
                  <PerformerImagesTab
                    performer={performer}
                    onImageUpdateChange={handleImageUpdateChange}
                  />
                ),
              },
            ]
          : []),
        ...(performer.gallery_count > 0
          ? [
              {
                id: "galleries",
                label: intl.formatMessage({
                  id: "galleries",
                  defaultMessage: "Galleries",
                }),
                content: <PerformerGalleriesTab performer={performer} />,
              },
            ]
          : []),
        ...(performer.group_count > 0
          ? [
              {
                id: "groups",
                label: intl.formatMessage({
                  id: "groups",
                  defaultMessage: "Groups",
                }),
                content: <PerformerGroupsTab performer={performer} />,
              },
            ]
          : []),
      ]
    : [];

  const activeTab = tab ?? entityTabs[0]?.id ?? "";
  function setActiveTab(id: string) {
    navigate({ search: { tab: id }, replace: true });
  }

  const title = performer
    ? performer.disambiguation
      ? `${performer.name} (${performer.disambiguation})`
      : performer.name
    : "";

  return (
    <>
      <DetailBackBar title={title} onBack={goBack} />
      {/* Mobile: single vertical scroll. Desktop (md+): split into a left
          sidebar holding the entity details (portrait + toolbar + details
          tab) and a right column holding the embedded list tabs — each
          column scrolls independently. */}
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden">
        <DetailPageState
          loading={loading}
          error={error}
          notFound={!performer}
          notFoundMessage={intl.formatMessage({
            id: "performer_not_found",
            defaultMessage: "Performer not found",
          })}
          skeletonProps={{ imageAspect: "aspect-[2/3]" }}
        >
          {performer && (
            <div className="md:h-full md:flex md:flex-row">
              <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border md:min-h-0">
                <DetailEditTransition
                  editing={editOpen}
                  fillHeight
                  detail={
                    <>
                      <DetailSidebarBack onBack={goBack} title={title} />
                      <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
                        <div className="flex flex-col items-stretch gap-3 p-3">
                          <PerformerPortrait
                            performer={performer}
                            onImageClick={() => setPortraitLightboxOpen(true)}
                            imageUpdating={imageUpdating}
                          />
                          <div className="min-w-0 md:order-first">
                            <PerformerToolbar
                              performer={performer}
                              onEdit={() => setEditOpen(true)}
                              onToggleFavorite={handleToggleFavorite}
                              onDeleted={goBack}
                            />
                          </div>
                        </div>
                        <div className="px-3 pb-3">
                          <PerformerDetailsTab performer={performer} />
                        </div>
                      </div>
                    </>
                  }
                  editForm={
                    <div className="flex flex-col h-full">
                      {/* Header sized to match `DetailSidebarBack` so
                          the swap between detail header and edit-form
                          header doesn't change the aside row height. */}
                      <div className="flex shrink-0 items-center gap-1 px-1 py-1 border-b border-border">
                        <Button
                          variant="ghost"
                          size="sm"
                          className="px-2 shrink-0"
                          onClick={() => setEditOpen(false)}
                          title={intl.formatMessage({
                            id: "actions.back",
                            defaultMessage: "Back",
                          })}
                        >
                          <ChevronLeft size={18} />
                        </Button>
                        <h2 className="text-base font-semibold leading-tight truncate min-w-0">
                          {intl.formatMessage(
                            {
                              id: "actions.edit_entity",
                              defaultMessage: "Edit {entityType}",
                            },
                            {
                              entityType: intl
                                .formatMessage({
                                  id: "performer",
                                  defaultMessage: "Performer",
                                })
                                .toLocaleLowerCase(),
                            },
                          )}
                        </h2>
                      </div>
                      {/* The form owns its own scroll body + anchored
                          action bar via flex-col layout, so we just
                          give it the remaining height of the aside. */}
                      <div className="flex-1 min-h-0">
                        <PerformerEditForm
                          performer={performer}
                          onSaved={() => setEditOpen(false)}
                        />
                      </div>
                    </div>
                  }
                />
              </aside>
              <div className="md:flex-1 md:min-w-0 md:min-h-0 md:flex md:flex-col">
                <DetailTabs
                  tabs={entityTabs}
                  activeTab={activeTab}
                  onTabChange={setActiveTab}
                />
              </div>
            </div>
          )}
        </DetailPageState>
      </div>

      {performer?.image_path && (
        <Lightbox
          open={portraitLightboxOpen}
          onClose={() => setPortraitLightboxOpen(false)}
          slides={[{ src: performer.image_path, alt: performer.name }]}
        />
      )}
    </>
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/performers/$performerId")({
  validateSearch: zodValidator(searchSchema),
  component: PerformerDetailPage,
});
