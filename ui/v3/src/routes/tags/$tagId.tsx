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
import { Tag, Heart, Pencil, ChevronLeft } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { TagDetailsTab } from "src/components/detail/tag-detail-tabs";
import { TagEditForm } from "src/components/detail/tag-edit-form";
import { TagActionsMenu } from "src/components/detail/tag-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import {
  TagScenesTab,
  TagImagesTab,
  TagGalleriesTab,
  TagPerformersTab,
  TagGroupsTab,
  TagStudiosTab,
  TagMarkersTab,
} from "src/components/detail/tag-list-tabs";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Tag image ─────────────────────────────────────────────────────────────────

type TagData = NonNullable<GQL.FindTagQuery["findTag"]>;

function TagImage({ tag }: { tag: TagData }) {
  const [failed, setFailed] = React.useState(false);
  // Default to landscape; we only need to switch on mobile if the image
  // turns out to be portrait. Fallback (no image) keeps the canonical
  // aspect-square placeholder.
  const [isPortrait, setIsPortrait] = React.useState(false);

  if (!tag.image_path || failed) {
    return (
      <div className="w-full shrink-0 aspect-square flex items-center justify-center bg-muted rounded text-muted-foreground">
        <Tag size={32} />
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
        src={tag.image_path}
        alt={tag.name}
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

// ── Tag toolbar ────────────────────────────────────────────────────────────────

function TagToolbar({
  tag,
  onEdit,
  onToggleFavorite,
  onDeleted,
}: {
  tag: TagData;
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
          tag.favorite &&
            "text-rose-500 border-rose-400/60 hover:text-rose-400",
        )}
        title={intl.formatMessage({
          id: "favourite",
          defaultMessage: "Favourite",
        })}
      >
        <Heart size={13} className={tag.favorite ? "fill-current" : ""} />
      </Button>
      <Button variant="outline" size="sm" onClick={onEdit}>
        <Pencil size={13} />
        {intl.formatMessage({ id: "actions.edit", defaultMessage: "Edit" })}
      </Button>
      <TagActionsMenu tag={tag} onDeleted={onDeleted} />
    </div>
  );
}

// ── Tag detail page ───────────────────────────────────────────────────────────

function TagDetailPage() {
  const { tagId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/tags");
  const intl = useIntl();
  const [editOpen, setEditOpen] = useState(false);

  const { data, loading, error } = useQuery(GQL.FindTagDocument, {
    variables: { id: tagId },
    fetchPolicy: "cache-first",
  });

  const [updateTag] = useMutation(GQL.TagUpdateDocument);

  const tag = data?.findTag;

  function handleToggleFavorite() {
    if (!tag) return;
    updateTag({
      variables: { input: { id: tag.id, favorite: !tag.favorite } },
      optimisticResponse: {
        tagUpdate: { ...tag, favorite: !tag.favorite },
      },
    });
  }

  type EntityTab = { id: string; label: string; content: React.ReactNode };
  const entityTabs: EntityTab[] = tag
    ? [
        ...(tag.scene_count > 0
          ? [
              {
                id: "scenes",
                label: intl.formatMessage({
                  id: "scenes",
                  defaultMessage: "Scenes",
                }),
                content: <TagScenesTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.scene_marker_count > 0
          ? [
              {
                id: "markers",
                label: intl.formatMessage({
                  id: "markers",
                  defaultMessage: "Markers",
                }),
                content: <TagMarkersTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.image_count > 0
          ? [
              {
                id: "images",
                label: intl.formatMessage({
                  id: "images",
                  defaultMessage: "Images",
                }),
                content: <TagImagesTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.gallery_count > 0
          ? [
              {
                id: "galleries",
                label: intl.formatMessage({
                  id: "galleries",
                  defaultMessage: "Galleries",
                }),
                content: <TagGalleriesTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.performer_count > 0
          ? [
              {
                id: "performers",
                label: intl.formatMessage({
                  id: "performers",
                  defaultMessage: "Performers",
                }),
                content: <TagPerformersTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.studio_count > 0
          ? [
              {
                id: "studios",
                label: intl.formatMessage({
                  id: "studios",
                  defaultMessage: "Studios",
                }),
                content: <TagStudiosTab tag={tag} />,
              },
            ]
          : []),
        ...(tag.group_count > 0
          ? [
              {
                id: "groups",
                label: intl.formatMessage({
                  id: "groups",
                  defaultMessage: "Groups",
                }),
                content: <TagGroupsTab tag={tag} />,
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
      <DetailBackBar title={tag?.name ?? ""} onBack={goBack} />
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden">
        <DetailPageState
          loading={loading}
          error={error}
          notFound={!tag}
          notFoundMessage={intl.formatMessage({
            id: "tag_not_found",
            defaultMessage: "Tag not found",
          })}
          skeletonProps={{ imageAspect: "aspect-square" }}
        >
          {tag && (
            <div className="md:h-full md:flex md:flex-row">
              <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border md:min-h-0">
                <DetailEditTransition
                  editing={editOpen}
                  fillHeight
                  detail={
                    <>
                      <DetailSidebarBack onBack={goBack} title={tag.name} />
                      <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
                        <div className="flex flex-col items-stretch gap-3 p-3">
                          <TagImage tag={tag} />
                          <div className="min-w-0 md:order-first">
                            <TagToolbar
                              tag={tag}
                              onEdit={() => setEditOpen(true)}
                              onToggleFavorite={handleToggleFavorite}
                              onDeleted={goBack}
                            />
                          </div>
                        </div>
                        <div className="px-3 pb-3">
                          <TagDetailsTab tag={tag} />
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
                                  id: "tag",
                                  defaultMessage: "Tag",
                                })
                                .toLocaleLowerCase(),
                            },
                          )}
                        </h2>
                      </div>
                      {/* Form owns its own scroll body + anchored
                          action bar via flex-col layout. */}
                      <div className="flex-1 min-h-0">
                        <TagEditForm
                          tag={tag}
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

export const Route = createFileRoute("/tags/$tagId")({
  validateSearch: zodValidator(searchSchema),
  component: TagDetailPage,
});
