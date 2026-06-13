import React, { useState } from "react";
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
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import { Images, Pencil, CheckCircle2Icon, ChevronLeft } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { galleryLabel } from "src/lib/gallery-utils";
import { GalleryDetailsTab } from "src/components/detail/gallery-detail-tabs";
import { GalleryActionsMenu } from "src/components/detail/gallery-actions-menu";
import { GalleryEditForm } from "src/components/detail/gallery-edit-form";
import { Lightbox } from "src/components/lightbox";
import { GalleryImagesTab } from "src/components/detail/gallery-list-tabs";
import { useDocumentTitle } from "src/hooks/title";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Gallery cover ─────────────────────────────────────────────────────────────

type GalleryData = NonNullable<GQL.FindGalleryQuery["findGallery"]>;

function GalleryCover({
  gallery,
  onImageClick,
}: {
  gallery: GalleryData;
  onImageClick?: () => void;
}) {
  const [failed, setFailed] = React.useState(false);
  const [isPortrait, setIsPortrait] = React.useState(false);

  if (!gallery.paths.cover || failed) {
    return (
      <div className="w-full shrink-0 aspect-square flex items-center justify-center bg-muted rounded text-muted-foreground">
        <Images size={32} />
      </div>
    );
  }
  return (
    <Button
      variant="ghost"
      className={cn(
        "shrink-0 overflow-hidden rounded p-0 h-auto cursor-zoom-in hover:bg-transparent",
        isPortrait ? "max-md:w-3/5 max-md:self-center md:w-full" : "w-full",
      )}
      onClick={onImageClick}
      aria-label="View full image"
    >
      <img
        src={gallery.paths.cover}
        alt={galleryLabel(gallery)}
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

// ── Gallery detail page ───────────────────────────────────────────────────────

function GalleryDetailPage() {
  const { galleryId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/galleries");
  const intl = useIntl();
  const [editOpen, setEditOpen] = useState(false);
  const [coverLightboxOpen, setCoverLightboxOpen] = useState(false);

  const { data, loading, error } = useQuery(GQL.FindGalleryDocument, {
    variables: { id: galleryId },
    fetchPolicy: "cache-first",
  });

  const [updateGallery] = useMutation(GQL.GalleryUpdateDocument);
  function handleToggleOrganized() {
    if (!gallery) return;
    updateGallery({
      variables: { input: { id: galleryId, organized: !gallery.organized } },
      optimisticResponse: {
        galleryUpdate: { ...gallery, organized: !gallery.organized },
      },
    });
  }

  const gallery = data?.findGallery;
  useDocumentTitle(gallery ? galleryLabel(gallery) : undefined);

  type EntityTab = { id: string; label: string; content: React.ReactNode };
  const entityTabs: EntityTab[] = gallery
    ? [
        ...(gallery.image_count > 0
          ? [
              {
                id: "images",
                label: intl.formatMessage({
                  id: "images",
                  defaultMessage: "Images",
                }),
                content: <GalleryImagesTab gallery={gallery} />,
              },
            ]
          : []),
      ]
    : [];

  const activeTab = tab ?? entityTabs[0]?.id ?? "";
  function setActiveTab(id: string) {
    navigate({ search: { tab: id }, replace: true });
  }

  return (
    <>
      <DetailBackBar
        title={gallery ? galleryLabel(gallery) : ""}
        onBack={goBack}
      />
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden">
        <DetailPageState
          loading={loading}
          error={error}
          notFound={!gallery}
          notFoundMessage={intl.formatMessage({
            id: "gallery_not_found",
            defaultMessage: "Gallery not found",
          })}
          skeletonProps={{ imageAspect: "aspect-square" }}
        >
          {gallery && (
            <div className="md:h-full md:flex md:flex-row">
              <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border md:min-h-0">
                <DetailEditTransition
                  editing={editOpen}
                  fillHeight
                  detail={
                    <>
                      <DetailSidebarBack
                        onBack={goBack}
                        title={galleryLabel(gallery)}
                      />
                      <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
                        <div className="flex flex-col items-stretch gap-3 p-3">
                          <GalleryCover
                            gallery={gallery}
                            onImageClick={() => setCoverLightboxOpen(true)}
                          />
                          <div className="min-w-0 md:order-first">
                            <div className="flex flex-wrap gap-2">
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setEditOpen(true)}
                              >
                                <Pencil size={13} />
                                {intl.formatMessage({
                                  id: "actions.edit",
                                  defaultMessage: "Edit",
                                })}
                              </Button>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={handleToggleOrganized}
                                className={cn(
                                  gallery.organized &&
                                    "text-green-600 border-green-500/60 hover:text-green-500",
                                )}
                                title={intl.formatMessage({
                                  id: "organized",
                                  defaultMessage: "Organized",
                                })}
                              >
                                <CheckCircle2Icon
                                  size={13}
                                  className={
                                    gallery.organized ? "fill-green-600/20" : ""
                                  }
                                />
                                {intl.formatMessage({
                                  id: "organized",
                                  defaultMessage: "Organized",
                                })}
                              </Button>
                              <GalleryActionsMenu
                                gallery={gallery}
                                onDeleted={goBack}
                              />
                            </div>
                          </div>
                        </div>
                        <div className="px-3 pb-3">
                          <GalleryDetailsTab gallery={gallery} />
                        </div>
                      </div>
                    </>
                  }
                  editForm={
                    <div className="flex flex-col h-full">
                      {/* Header sized to match `DetailSidebarBack` so the
                          swap between detail header and edit-form header
                          doesn't change the aside row height. */}
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
                                  id: "gallery",
                                  defaultMessage: "Gallery",
                                })
                                .toLocaleLowerCase(),
                            },
                          )}
                        </h2>
                      </div>
                      <div className="flex-1 min-h-0">
                        <GalleryEditForm
                          gallery={gallery}
                          onSaved={() => setEditOpen(false)}
                          onDeleted={goBack}
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

      {gallery?.paths.cover && (
        <Lightbox
          open={coverLightboxOpen}
          onClose={() => setCoverLightboxOpen(false)}
          slides={[{ src: gallery.paths.cover, alt: galleryLabel(gallery) }]}
        />
      )}
    </>
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/galleries/$galleryId")({
  validateSearch: zodValidator(searchSchema),
  component: GalleryDetailPage,
});
