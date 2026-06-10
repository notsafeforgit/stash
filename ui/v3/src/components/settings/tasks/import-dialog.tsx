import { useRef, useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import { FileUp, Upload } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import { Spinner } from "src/components/ui/spinner";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
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
import { useToast } from "src/hooks/toast";

interface IProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const DUPLICATE_LABELS: Record<GQL.ImportDuplicateEnum, string> = {
  [GQL.ImportDuplicateEnum.Fail]: "Fail",
  [GQL.ImportDuplicateEnum.Ignore]: "Ignore",
  [GQL.ImportDuplicateEnum.Overwrite]: "Overwrite",
};

const MISSING_REF_LABELS: Record<GQL.ImportMissingRefEnum, string> = {
  [GQL.ImportMissingRefEnum.Fail]: "Fail",
  [GQL.ImportMissingRefEnum.Ignore]: "Ignore",
  [GQL.ImportMissingRefEnum.Create]: "Create",
};

/**
 * Incremental import from a user-supplied zip — i.e. one previously
 * produced by `export` to a path outside the metadata directory. Distinct
 * from "Full import" which reads from the configured metadata directory.
 */
export function ImportDialog({ open, onOpenChange }: IProps) {
  const intl = useIntl();
  const toast = useToast();
  const [duplicateBehaviour, setDuplicateBehaviour] =
    useState<GQL.ImportDuplicateEnum>(GQL.ImportDuplicateEnum.Ignore);
  const [missingRefBehaviour, setMissingRefBehaviour] =
    useState<GQL.ImportMissingRefEnum>(GQL.ImportMissingRefEnum.Fail);
  const [file, setFile] = useState<File | undefined>();
  const [running, setRunning] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const [importObjects] = useMutation(GQL.ImportObjectsDocument);

  function reset() {
    setFile(undefined);
    setDuplicateBehaviour(GQL.ImportDuplicateEnum.Ignore);
    setMissingRefBehaviour(GQL.ImportMissingRefEnum.Fail);
    if (fileInputRef.current) fileInputRef.current.value = "";
  }

  function handleOpenChange(o: boolean) {
    if (running) return;
    if (!o) reset();
    onOpenChange(o);
  }

  async function onImport() {
    if (!file) return;
    setRunning(true);
    try {
      await importObjects({
        variables: {
          input: { duplicateBehaviour, missingRefBehaviour, file },
        },
      });
      toast.success(
        intl.formatMessage({
          id: "toast.started_importing",
          defaultMessage: "Started importing",
        }),
      );
      onOpenChange(false);
      reset();
    } catch (e) {
      toast.error(e);
    } finally {
      setRunning(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Upload className="size-4 text-muted-foreground" />
            <FormattedMessage
              id="actions.import_from_file"
              defaultMessage="Import from file"
            />
          </DialogTitle>
          <DialogDescription>
            <FormattedMessage
              id="config.tasks.incremental_import"
              defaultMessage="Incremental import from a supplied export zip file."
            />
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="import-file">
              {intl.formatMessage({
                id: "dialogs.import.import_zip_file",
                defaultMessage: "Import zip file",
              })}
            </Label>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => fileInputRef.current?.click()}
                disabled={running}
              >
                <FileUp className="size-4" />
                <FormattedMessage
                  id="actions.choose_file"
                  defaultMessage="Choose file"
                />
              </Button>
              <span className="truncate text-sm text-muted-foreground">
                {file?.name ??
                  intl.formatMessage({
                    id: "dialogs.import.no_file_chosen",
                    defaultMessage: "No file chosen",
                  })}
              </span>
            </div>
            <Input
              ref={fileInputRef}
              id="import-file"
              type="file"
              accept=".zip"
              hidden
              onChange={(e) => {
                if (e.target.files && e.target.files.length > 0) {
                  setFile(e.target.files[0]);
                }
              }}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="import-duplicate-behaviour">
              {intl.formatMessage({
                id: "dialogs.import.duplicate_object_handling",
                defaultMessage: "Duplicate object handling",
              })}
            </Label>
            <Select
              value={duplicateBehaviour}
              onValueChange={(v) =>
                setDuplicateBehaviour(v as GQL.ImportDuplicateEnum)
              }
              disabled={running}
            >
              <SelectTrigger id="import-duplicate-behaviour" className="w-40">
                <SelectValue>
                  {DUPLICATE_LABELS[duplicateBehaviour]}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {Object.values(GQL.ImportDuplicateEnum).map((v) => (
                  <SelectItem key={v} value={v}>
                    {DUPLICATE_LABELS[v]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label htmlFor="import-missing-ref">
              {intl.formatMessage({
                id: "dialogs.import.missing_reference_handling",
                defaultMessage: "Missing reference handling",
              })}
            </Label>
            <Select
              value={missingRefBehaviour}
              onValueChange={(v) =>
                setMissingRefBehaviour(v as GQL.ImportMissingRefEnum)
              }
              disabled={running}
            >
              <SelectTrigger id="import-missing-ref" className="w-40">
                <SelectValue>
                  {MISSING_REF_LABELS[missingRefBehaviour]}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {Object.values(GQL.ImportMissingRefEnum).map((v) => (
                  <SelectItem key={v} value={v}>
                    {MISSING_REF_LABELS[v]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter>
          <DialogClose
            render={
              <Button type="button" variant="outline" disabled={running}>
                <FormattedMessage id="actions.cancel" defaultMessage="Cancel" />
              </Button>
            }
          />
          <Button
            type="button"
            variant="destructive"
            disabled={!file || running}
            onClick={() => void onImport()}
          >
            {running && <Spinner className="size-4" />}
            <FormattedMessage id="actions.import" defaultMessage="Import" />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
