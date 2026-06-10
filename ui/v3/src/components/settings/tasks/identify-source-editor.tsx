import { useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { Plus, Pencil } from "lucide-react";
import type * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Label } from "src/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import type { IScraperSource } from "./identify-types";
import { IdentifyOptionsEditor } from "./identify-options-editor";

interface IProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** When undefined, dialog is in "Add" mode and lets the user pick a scraper. */
  source?: IScraperSource;
  availableSources: IScraperSource[];
  defaultOptions: GQL.IdentifyMetadataOptionsInput;
  onSave: (s: IScraperSource) => void;
}

export function IdentifySourceEditor({
  open,
  onOpenChange,
  source,
  availableSources,
  defaultOptions,
  onSave,
}: IProps) {
  const intl = useIntl();
  const isNew = !source;

  const initial: IScraperSource = source ??
    availableSources[0] ?? {
      id: "",
      displayName: "",
    };

  const [working, setWorking] = useState<IScraperSource>(initial);

  // Reset when the dialog opens with a different source.
  function handleOpenChange(o: boolean) {
    if (o) setWorking(initial);
    onOpenChange(o);
  }

  function pickSource(id: string) {
    const next = availableSources.find((s) => s.id === id);
    if (!next) return;
    setWorking({ ...working, ...next });
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            {isNew ? (
              <Plus className="size-4" />
            ) : (
              <Pencil className="size-4" />
            )}
            {isNew
              ? intl.formatMessage({
                  id: "actions.add",
                  defaultMessage: "Add",
                })
              : working.displayName}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {isNew && (
            <div className="space-y-2">
              <Label htmlFor="source-pick">
                {intl.formatMessage({
                  id: "config.tasks.identify.source",
                  defaultMessage: "Source",
                })}
              </Label>
              <Select
                value={working.id}
                onValueChange={(v) => v && pickSource(v)}
              >
                <SelectTrigger id="source-pick" className="w-full">
                  <SelectValue>{working.displayName}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {availableSources.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.displayName}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <IdentifyOptionsEditor
            options={working.options ?? {}}
            setOptions={(o) => setWorking({ ...working, options: o })}
            source={{ displayName: working.displayName }}
            defaultOptions={defaultOptions}
          />
        </div>

        <DialogFooter>
          <DialogClose
            render={
              <Button type="button" variant="outline">
                <FormattedMessage id="actions.cancel" defaultMessage="Cancel" />
              </Button>
            }
          />
          <Button
            type="button"
            disabled={!working.scraper_id && !working.stash_box_endpoint}
            onClick={() => {
              onSave(working);
              onOpenChange(false);
            }}
          >
            <FormattedMessage
              id={isNew ? "actions.add" : "actions.confirm"}
              defaultMessage={isNew ? "Add" : "Confirm"}
            />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
