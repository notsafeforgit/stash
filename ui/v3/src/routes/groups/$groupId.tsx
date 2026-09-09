import { EntityActionButton } from "@/components/detail/entity-actions-menu";
import React, { useState } from "react";
import { cn } from "src/lib/utils";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery } from "@apollo/client/react";
import { z } from "zod";
import { useIntl } from "react-intl";
import {
  DetailSidebarBack,
  DetailPageState,
} from "src/components/detail/detail-page-parts";
import {
  DetailTabs,
  type DetailTabsTab,
} from "src/components/detail/detail-tabs";
import { CollectionDetailLayout } from "@/components/detail/collection-detail-layout";
import { MobileDetailChromePortal } from "@/components/layout/mobile-detail-chrome";
import { Film, Pencil, Clapperboard, Users } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { GroupDetailsTab } from "src/components/detail/group-detail-tabs";
import { GroupActionsMenu } from "src/components/detail/group-actions-menu";
import { GroupEditForm } from "src/components/detail/group-edit-form";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import { DetailEditorLayout } from "@/components/detail/detail-editor-layout";
import { useDocumentTitle } from "src/hooks/title";
import {
  GroupScenesTab,
  GroupPerformersTab,
} from "src/components/detail/group-list-tabs";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Group image ────────────────────────────────────────────────────────────────

type GroupData = NonNullable<GQL.FindGroupQuery["findGroup"]>;

function GroupImage({ group }: { group: GroupData }) {
  const [failed, setFailed] = React.useState(false);
  // Group front images are typically portrait posters; default to portrait
  // and let the natural-aspect detection on load correct it if it's not.
  const [isPortrait, setIsPortrait] = React.useState(true);

  if (!group.front_image_path || failed) {
    return (
      <div className="max-md:w-3/5 max-md:self-center md:w-full shrink-0 aspect-[2/3] flex items-center justify-center bg-muted rounded text-muted-foreground">
        <Film size={32} />
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
        src={group.front_image_path}
        alt={group.name}
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

// ── Group detail page ─────────────────────────────────────────────────────────

function GroupDetailPage() {
  const { groupId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/groups");
  const intl = useIntl();
  const [editOpen, setEditOpen] = useState(false);

  const { data, loading, error } = useQuery(GQL.FindGroupDocument, {
    variables: { id: groupId },
    fetchPolicy: "cache-first",
  });

  const group = data?.findGroup;
  useDocumentTitle(group?.name);

  const entityTabs: DetailTabsTab[] = group
    ? [
        ...(group.scene_count > 0
          ? [
              {
                id: "scenes",
                icon: Clapperboard,
                label: intl.formatMessage({
                  id: "scenes",
                  defaultMessage: "Scenes",
                }),
                content: <GroupScenesTab group={group} />,
              },
            ]
          : []),
        ...(group.performer_count > 0
          ? [
              {
                id: "performers",
                icon: Users,
                label: intl.formatMessage({
                  id: "performers",
                  defaultMessage: "Performers",
                }),
                content: <GroupPerformersTab group={group} />,
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
    <CollectionDetailLayout title={group?.name ?? ""} onBack={goBack}>
      <DetailPageState
        loading={loading}
        error={error}
        notFound={!group}
        notFoundMessage={intl.formatMessage({
          id: "group_not_found",
          defaultMessage: "Group not found",
        })}
        skeletonProps={{ imageAspect: "aspect-[2/3]" }}
      >
        {group && (
          <div className="md:h-full md:flex md:flex-row">
            <aside className="md:w-72 lg:w-80 md:shrink-0 md:flex md:flex-col md:border-r md:border-border md:min-h-0">
              <DetailEditTransition
                editing={editOpen}
                fillHeight
                detail={
                  <>
                    <DetailSidebarBack onBack={goBack} title={group.name} />
                    <div className="md:flex-1 md:min-h-0 md:overflow-y-auto">
                      <div className="flex flex-col items-stretch gap-3 p-3">
                        <GroupImage group={group} />
                        <MobileDetailChromePortal slot="actions">
                          <div className="min-w-0 md:order-first">
                            <div className="flex flex-wrap gap-2">
                              <EntityActionButton
                                icon={Pencil}
                                label={intl.formatMessage({
                                  id: "actions.edit",
                                  defaultMessage: "Edit",
                                })}
                                onClick={() => setEditOpen(true)}
                              />
                              <GroupActionsMenu
                                group={group}
                                onDeleted={goBack}
                              />
                            </div>
                          </div>
                        </MobileDetailChromePortal>
                      </div>
                      <div className="px-3 pb-3">
                        <GroupDetailsTab group={group} />
                      </div>
                    </div>
                  </>
                }
                editForm={
                  <DetailEditorLayout
                    onClose={() => setEditOpen(false)}
                    title={intl.formatMessage(
                      {
                        id: "actions.edit_entity",
                        defaultMessage: "Edit {entityType}",
                      },
                      {
                        entityType: intl
                          .formatMessage({
                            id: "group",
                            defaultMessage: "Group",
                          })
                          .toLocaleLowerCase(),
                      },
                    )}
                  >
                    <GroupEditForm
                      group={group}
                      onSaved={() => setEditOpen(false)}
                    />
                  </DetailEditorLayout>
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
    </CollectionDetailLayout>
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/groups/$groupId")({
  validateSearch: searchSchema,
  component: GroupDetailPage,
});
