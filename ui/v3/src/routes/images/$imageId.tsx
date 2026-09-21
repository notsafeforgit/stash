import { ImageViewer } from "@/components/detail/image-viewer";
import { EntityActionButton } from "@/components/detail/entity-actions-menu";

import React, { useState } from "react";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useSmartBack } from "src/hooks/use-smart-back";
import { useQuery, useMutation } from "@apollo/client/react";
import { z } from "zod";
import { useIntl } from "react-intl";
import { Spinner } from "src/components/ui/spinner";
import { Button } from "src/components/ui/button";
import { cn } from "src/lib/utils";
import {
  Star,
  CheckCircle2Icon,
  Droplets,
  Minus,
  RotateCcw,
  Pencil,
  FileText,
  Info,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { imageTitle } from "src/core/files";
import {
  MediaDetailLayout,
  type DetailTab,
} from "src/components/detail/media-detail-layout";
import {
  ImageDetailsTab,
  ImageFileInfoTab,
} from "src/components/detail/image-detail-tabs";
import { ImageEditForm } from "src/components/detail/image-edit-form";
import { ImageActionsMenu } from "src/components/detail/image-actions-menu";
import { DetailEditTransition } from "src/components/detail/detail-edit-transition";
import { DetailEditorLayout } from "@/components/detail/detail-editor-layout";

import { useImageOCounter } from "src/hooks/use-image-o-counter";
import { useDocumentTitle } from "src/hooks/title";

// ── Route search params ────────────────────────────────────────────────────────

const searchSchema = z.object({
  tab: z.string().optional(),
});

// ── Image viewer ──────────────────────────────────────────────────────────────

type ImageData = NonNullable<GQL.FindImageQuery["findImage"]>;

// ── Image toolbar ─────────────────────────────────────────────────────────────

interface ImageToolbarProps {
  image: ImageData;
  onEdit: () => void;
  onAddO: () => void;
  onSubO: () => void;
  onResetO: () => void;
  onToggleOrganized: () => void;
  onDeleted?: () => void;
}

function ImageToolbar({
  image,
  onEdit,
  onAddO,
  onSubO,
  onResetO,
  onToggleOrganized,
  onDeleted,
}: ImageToolbarProps) {
  const intl = useIntl();
  const oCounter = image.o_counter ?? 0;

  return (
    <div className="flex flex-wrap items-center gap-2 lg:gap-3 py-1.5">
      <div className="flex flex-wrap items-center gap-1">
        {image.rating100 != null && (
          <span
            className="inline-flex items-center bg-transparent border border-border rounded-md text-muted-foreground text-[0.8125rem] gap-1 px-2 py-1 mr-1"
            title={intl.formatMessage({
              id: "rating",
              defaultMessage: "Rating",
            })}
          >
            <Star size={14} />
            {image.rating100}
          </span>
        )}
        {/* O-counter: images don't track an o_history, so decrement and
            reset live alongside the +1 increment in the toolbar instead
            of behind the edit form. */}
        <Button
          variant="outline"
          className="h-auto bg-transparent px-2 py-1 text-[0.8125rem] gap-1 text-muted-foreground hover:text-foreground"
          onClick={onAddO}
          title={intl.formatMessage({
            id: "o_counter",
            defaultMessage: "Add O",
          })}
        >
          <Droplets size={14} />
          {oCounter}
        </Button>
        <Button
          variant="outline"
          size="icon-sm"
          className="bg-transparent text-muted-foreground hover:text-foreground"
          onClick={onSubO}
          disabled={oCounter <= 0}
          title={intl.formatMessage({
            id: "actions.decrement_o",
            defaultMessage: "Decrement O",
          })}
        >
          <Minus size={13} />
        </Button>
        {oCounter > 0 && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground hover:text-foreground"
            onClick={onResetO}
            title={intl.formatMessage({
              id: "actions.reset_o",
              defaultMessage: "Reset O",
            })}
          >
            <RotateCcw size={13} />
          </Button>
        )}
      </div>

      <Button
        variant="outline"
        size="sm"
        onClick={onToggleOrganized}
        className={cn(
          image.organized &&
            "text-green-600 border-green-500/60 hover:text-green-500",
        )}
        title={intl.formatMessage({
          id: "organized",
          defaultMessage: "Organized",
        })}
      >
        <CheckCircle2Icon
          size={13}
          className={image.organized ? "fill-green-600/20" : ""}
        />
        <span>
          {intl.formatMessage({ id: "organized", defaultMessage: "Organized" })}
        </span>
      </Button>

      <EntityActionButton
        icon={Pencil}
        label={intl.formatMessage({ id: "actions.edit" })}
        className="lg:hidden"
        onClick={onEdit}
      />
      <div className="w-full lg:ml-auto lg:w-auto">
        <ImageActionsMenu image={image} onDeleted={onDeleted} />
      </div>
    </div>
  );
}

// ── Image detail page ─────────────────────────────────────────────────────────

function ImageDetailPage() {
  const { imageId } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const goBack = useSmartBack("/images");
  const intl = useIntl();

  const { data, loading, error } = useQuery(GQL.FindImageDocument, {
    variables: { id: imageId },
    fetchPolicy: "cache-first",
  });

  const [updateImage] = useMutation(GQL.ImageUpdateDocument);
  function handleToggleOrganized() {
    if (!image) return;
    updateImage({
      variables: { input: { id: imageId, organized: !image.organized } },
      optimisticResponse: {
        imageUpdate: { ...image, organized: !image.organized },
      },
    });
  }

  const { incrementO, decrementO, resetO } = useImageOCounter(imageId);

  const activeTab = tab ?? "details";
  function setActiveTab(id: string) {
    navigate({ search: (prev) => ({ ...prev, tab: id }), replace: true });
  }

  // Inline edit transition on the Details tab — replaces the previous
  // top-level "Edit" tab. See `routes/scenes/$sceneId.tsx` for the
  // full rationale; reset to false whenever the user navigates away
  // from Details so re-entering the tab lands on the read-only view.
  const [editingDetails, setEditingDetails] = useState(false);
  React.useEffect(() => {
    if (activeTab !== "details") setEditingDetails(false);
  }, [activeTab]);

  const image = data?.findImage;
  useDocumentTitle(image ? imageTitle(image) || undefined : undefined);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-48 text-muted-foreground">
        <Spinner className="size-10" />
      </div>
    );
  }

  if (error || !image) {
    return (
      <div className="p-4 text-destructive">
        {error?.message ??
          intl.formatMessage({
            id: "image_not_found",
            defaultMessage: "Image not found",
          })}
      </div>
    );
  }

  const tabs: DetailTab[] = [
    {
      id: "details",
      icon: Info,
      label: intl.formatMessage({ id: "details", defaultMessage: "Details" }),
      shortcut: "a",
      content: (
        <DetailEditTransition
          editing={editingDetails}
          detail={
            <div className="flex flex-col gap-3">
              <div className="hidden lg:flex justify-end">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setEditingDetails(true)}
                >
                  <Pencil size={13} />
                  {intl.formatMessage({
                    id: "actions.edit",
                    defaultMessage: "Edit",
                  })}
                </Button>
              </div>
              <ImageDetailsTab image={image} />
            </div>
          }
          editForm={
            <DetailEditorLayout
              onClose={() => setEditingDetails(false)}
              title={intl.formatMessage(
                {
                  id: "actions.edit_entity",
                  defaultMessage: "Edit {entityType}",
                },
                {
                  entityType: intl
                    .formatMessage({
                      id: "image",
                      defaultMessage: "Image",
                    })
                    .toLocaleLowerCase(),
                },
              )}
            >
              <ImageEditForm
                image={image}
                onSaved={() => setEditingDetails(false)}
              />
            </DetailEditorLayout>
          }
        />
      ),
    },
    {
      id: "fileinfo",
      icon: FileText,
      label: intl.formatMessage({
        id: "file_info",
        defaultMessage: "File info",
      }),
      shortcut: "i",
      content: <ImageFileInfoTab image={image} />,
    },
  ];

  return (
    <MediaDetailLayout
      title={imageTitle(image) || undefined}
      primaryContent={<ImageViewer image={image} />}
      headerContent={
        <ImageToolbar
          image={image}
          onEdit={() => {
            setActiveTab("details");
            setEditingDetails(true);
          }}
          onAddO={() => incrementO()}
          onSubO={() => decrementO()}
          onResetO={() => resetO()}
          onToggleOrganized={handleToggleOrganized}
          onDeleted={goBack}
        />
      }
      tabs={tabs}
      activeTab={activeTab}
      onTabChange={setActiveTab}
      onBack={goBack}
      mobilePageScroll
    />
  );
}

// ── Route ─────────────────────────────────────────────────────────────────────

export const Route = createFileRoute("/images/$imageId")({
  validateSearch: searchSchema,
  component: ImageDetailPage,
});
