import React, { useState } from "react";
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
import { Button } from "src/components/ui/button";
import { Building2, Star, Heart, Pencil, ChevronLeft } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { StudioDetailsTab } from "src/components/detail/studio-detail-tabs";
import { StudioEditForm } from "src/components/detail/studio-edit-form";
import { StudioActionsMenu } from "src/components/detail/studio-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import { useDocumentTitle } from "src/hooks/title";
import { useConfigurationContextOptional } from "src/hooks/config";
import {
  StudioScenesTab,
  StudioImagesTab,
  StudioGalleriesTab,
  StudioPerformersTab,
  StudioGroupsTab,
} from "src/components/detail/studio-list-tabs";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Studio image ──────────────────────────────────────────────────────────────

type StudioData = NonNullable<GQL.FindStudioQuery["findStudio"]>;

function StudioImage({ studio }: { studio: StudioData }) {
  const [failed, setFailed] = React.useState(false);
  const [isPortrait, setIsPortrait] = React.useState(false);

  if (!studio.image_path || failed) {
    return (
      <div className="w-full shrink-0 aspect-square flex items-center justify-center bg-muted rounded text-muted-foreground">
        <Building2 size={32} />
      </div>
    );
  }
  return (
    <div
      className={cn(
        "shrink-0 overflow-hidden rounded",
        isPortrait ? "max-md:w-3/5 max-md:self-center md:w-full" : "w-full",
      )}
    >
      <img
        src={studio.image_path}
        alt={studio.name}
        className="w-full h-auto"
        onLoad={(e) => {
          const img = e.currentTarget;
          setIsPortrait(img.naturalHeight > img.naturalWidth);
        }}
        onError={() => setFailed(true)}
      />
    </div>
  );
}

// ── Studio toolbar ────────────────────────────────────────────────────────────

function StudioToolbar({
  studio,
  onEdit,
  onToggleFavorite,
  onDeleted,
}: {
  studio: StudioData;
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
          studio.favorite &&
            "text-rose-500 border-rose-400/60 hover:text-rose-400",
        )}
        title={intl.formatMessage({
          id: "favourite",
          defaultMessage: "Favourite",
        })}
      >
        <Heart size={13} className={studio.favorite ? "fill-current" : ""} />
      </Button>
      {studio.rating100 != null && (
        <span
          className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1 select-none"
          title={intl.formatMessage({
            id: "rating",
            defaultMessage: "Rating",
          })}
        >
          <Star size={13} />
          {studio.rating100}
        </span>
      )}
      <Button variant="outline" size="sm" onClick={onEdit}>
        <Pencil size={13} />
        {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
      </Button>
      <StudioActionsMenu studio={studio} onDeleted={onDeleted} />
    </div>
  );
}

// ── Studio detail page ────────────────────────────────────────────────────────

function StudioDetailPage() {
  const { studioId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/studios");
  const intl = useIntl();
  const [editOpen, setEditOpen] = useState(false);
  const showChildStudioContent =
    useConfigurationContextOptional()?.configuration.ui
      .showChildStudioContent ?? false;

  const { data, loading, error } = useQuery(GQL.FindStudioDocument, {
    variables: { id: studioId },
    fetchPolicy: "cache-first",
  });

  const [updateStudio] = useMutation(GQL.StudioUpdateDocument);

  const studio = data?.findStudio;
  useDocumentTitle(studio?.name);

  const sceneCount = studio
    ? showChildStudioContent
      ? studio.scene_count_all
      : studio.scene_count
    : 0;
  const imageCount = studio
    ? showChildStudioContent
      ? studio.image_count_all
      : studio.image_count
    : 0;
  const galleryCount = studio
    ? showChildStudioContent
      ? studio.gallery_count_all
      : studio.gallery_count
    : 0;
  const performerCount = studio
    ? showChildStudioContent
      ? studio.performer_count_all
      : studio.performer_count
    : 0;
  const groupCount = studio
    ? showChildStudioContent
      ? studio.group_count_all
      : studio.group_count
    : 0;

  function handleToggleFavorite() {
    if (!studio) return;
    updateStudio({
      variables: { input: { id: studio.id, favorite: !studio.favorite } },
      optimisticResponse: {
        studioUpdate: { ...studio, favorite: !studio.favorite },
      },
    });
  }

  type EntityTab = { id: string; label: string; content: React.ReactNode };
  const entityTabs: EntityTab[] = studio
    ? [
        ...(sceneCount > 0
          ? [
              {
                id: "scenes",
                label: intl.formatMessage({
                  id: "scenes",
                  defaultMessage: "Scenes",
                }),
                content: <StudioScenesTab studio={studio} />,
              },
            ]
          : []),
        ...(imageCount > 0
          ? [
              {
                id: "images",
                label: intl.formatMessage({
                  id: "images",
                  defaultMessage: "Images",
                }),
                content: <StudioImagesTab studio={studio} />,
              },
            ]
          : []),
        ...(galleryCount > 0
          ? [
              {
                id: "galleries",
                label: intl.formatMessage({
                  id: "galleries",
                  defaultMessage: "Galleries",
                }),
                content: <StudioGalleriesTab studio={studio} />,
              },
            ]
          : []),
        ...(performerCount > 0
          ? [
              {
                id: "performers",
                label: intl.formatMessage({
                  id: "performers",
                  defaultMessage: "Performers",
                }),
                content: <StudioPerformersTab studio={studio} />,
              },
            ]
          : []),
        ...(groupCount > 0
          ? [
              {
                id: "groups",
                label: intl.formatMessage({
                  id: "groups",
                  defaultMessage: "Groups",
                }),
                content: <StudioGroupsTab studio={studio} />,
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
      <DetailBackBar title={studio?.name ?? ""} onBack={goBack} />
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden">
        <DetailPageState
          loading={loading}
          error={error}
          notFound={!studio}
          notFoundMessage={intl.formatMessage({
            id: "studio_not_found",
            defaultMessage: "Studio not found",
          })}
          skeletonProps={{ imageAspect: "aspect-square" }}
        >
          {studio && (
            <div className="md:h-full md:flex md:flex-row">
              <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border md:min-h-0">
                <DetailEditTransition
                  editing={editOpen}
                  fillHeight
                  detail={
                    <>
                      <DetailSidebarBack onBack={goBack} title={studio.name} />
                      <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
                        <div className="flex flex-col items-stretch gap-3 p-3">
                          <StudioImage studio={studio} />
                          <div className="min-w-0 md:order-first">
                            <StudioToolbar
                              studio={studio}
                              onEdit={() => setEditOpen(true)}
                              onToggleFavorite={handleToggleFavorite}
                              onDeleted={goBack}
                            />
                          </div>
                        </div>
                        <div className="px-3 pb-3">
                          <StudioDetailsTab studio={studio} />
                        </div>
                      </div>
                    </>
                  }
                  editForm={
                    <div className="flex flex-col h-full">
                      {/* Header sized to match `DetailSidebarBack`. */}
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
                                  id: "studio",
                                  defaultMessage: "Studio",
                                })
                                .toLocaleLowerCase(),
                            },
                          )}
                        </h2>
                      </div>
                      {/* The form owns its own scroll body + anchored
                          action bar via flex-col layout. */}
                      <div className="flex-1 min-h-0">
                        <StudioEditForm
                          studio={studio}
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
    </>
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/studios/$studioId")({
  validateSearch: zodValidator(searchSchema),
  component: StudioDetailPage,
});
