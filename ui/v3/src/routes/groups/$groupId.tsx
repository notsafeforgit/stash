import React, { useState } from "react";
import { cn } from "src/lib/utils";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery } from "@apollo/client/react";
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
import { Film, Pencil, ChevronLeft } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { GroupDetailsTab } from "src/components/detail/group-detail-tabs";
import { GroupActionsMenu } from "src/components/detail/group-actions-menu";
import { GroupEditForm } from "src/components/detail/group-edit-form";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
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

  type EntityTab = { id: string; label: string; content: React.ReactNode };
  const entityTabs: EntityTab[] = group
    ? [
        ...(group.scene_count > 0
          ? [
              {
                id: "scenes",
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
    <>
      <DetailBackBar title={group?.name ?? ""} onBack={goBack} />
      <div className="flex-1 min-h-0 overflow-y-auto md:overflow-hidden">
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
                              <GroupActionsMenu
                                group={group}
                                onDeleted={goBack}
                              />
                            </div>
                          </div>
                        </div>
                        <div className="px-3 pb-3">
                          <GroupDetailsTab group={group} />
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
                                  id: "group",
                                  defaultMessage: "Group",
                                })
                                .toLocaleLowerCase(),
                            },
                          )}
                        </h2>
                      </div>
                      {/* Form owns its own scroll body + anchored
                          action bar via flex-col layout. */}
                      <div className="flex-1 min-h-0">
                        <GroupEditForm
                          group={group}
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

export const Route = createFileRoute("/groups/$groupId")({
  validateSearch: zodValidator(searchSchema),
  component: GroupDetailPage,
});
