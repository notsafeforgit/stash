import { useState } from "react";
import { Copy, Eye } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { useMsg } from "@/hooks/message";
import { useToast } from "@/hooks/toast";

export function ShareLinkDialog({
  link,
  onClose,
}: {
  link: { id: string; url: string };
  onClose: () => void;
}) {
  const msg = useMsg();
  const toast = useToast();
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(link.url);
      setCopied(true);
    } catch (error) {
      toast.error(error);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[calc(100dvh-2rem)] grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto]">
        <DialogHeader>
          <DialogTitle>
            {msg("sharing.link_ready", "Your share link is ready")}
          </DialogTitle>
        </DialogHeader>
        <FieldGroup className="-m-1 min-h-0 min-w-0 overflow-x-hidden overflow-y-auto overscroll-contain p-1">
          <DialogDescription>
            {msg(
              "sharing.link_secret",
              "Anyone with this link can view the selected media until it expires or you revoke it. You can copy this same link again from Settings → Shares.",
            )}
          </DialogDescription>
          <Field>
            <FieldLabel htmlFor="share-result-link">
              {msg("sharing.link", "Share link")}
            </FieldLabel>
            <Input
              id="share-result-link"
              value={link.url}
              readOnly
              autoComplete="off"
              onFocus={(event) => event.currentTarget.select()}
            />
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() =>
              window.open(link.url, "_blank", "noopener,noreferrer")
            }
          >
            <Eye data-icon="inline-start" />
            {msg("sharing.preview", "Recipient preview")}
          </Button>
          <Button onClick={() => void copy()}>
            <Copy data-icon="inline-start" />
            {copied
              ? msg("sharing.copied", "Copied")
              : msg("sharing.copy", "Copy link")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
