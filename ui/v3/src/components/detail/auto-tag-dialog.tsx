import { useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { Wand2 } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { AutoTagWarning } from "src/components/shared/auto-tag-warning";
import { useToast } from "src/hooks/toast";

export type AutoTagEntityType = "performer" | "tag" | "studio";

interface AutoTagDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  entityType: AutoTagEntityType;
  /** IDs to auto tag against. */
  ids: string[];
  /** Optional name to display in the title; only shown when ids.length === 1. */
  entityName?: string;
}

export function AutoTagDialog({
  open,
  onOpenChange,
  entityType,
  ids,
  entityName,
}: AutoTagDialogProps) {
  const intl = useIntl();
  const toast = useToast();
  const [submitting, setSubmitting] = useState(false);
  const [autoTag] = useMutation(GQL.MetadataAutoTagDocument);

  const isSingle = ids.length === 1;

  const title =
    isSingle && entityName
      ? intl.formatMessage(
          {
            id: "dialogs.auto_tag.entity_title",
            defaultMessage: 'Auto tag "{name}"?',
          },
          { name: entityName },
        )
      : intl.formatMessage(
          {
            id: "dialogs.auto_tag.count_title",
            defaultMessage:
              "Auto tag {count, plural, one {# {entityType}} other {# {entityType}s}}?",
          },
          { count: ids.length, entityType },
        );

  async function handleConfirm() {
    setSubmitting(true);
    try {
      const input: GQL.AutoTagMetadataInput =
        entityType === "performer"
          ? { performers: ids }
          : entityType === "tag"
            ? { tags: ids }
            : { studios: ids };
      await autoTag({ variables: { input } });
      toast.success(
        intl.formatMessage({
          id: "toast.started_auto_tagging",
          defaultMessage: "Started auto tagging",
        }),
      );
      onOpenChange(false);
    } catch (e) {
      toast.error(e);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={submitting ? () => {} : onOpenChange}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        <AutoTagWarning />

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button size="sm" disabled={submitting} onClick={handleConfirm}>
            {submitting ? <Spinner className="size-4" /> : <Wand2 />}
            {intl.formatMessage({
              id: "actions.auto_tag",
              defaultMessage: "Auto tag",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
