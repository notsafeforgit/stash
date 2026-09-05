import { useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { Minus, Plus } from "lucide-react";
import { Button } from "src/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { useConfigurationContext } from "src/hooks/config";
import { FolderSelect } from "./folder-select";

interface IProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (paths: string[]) => void;
  title?: React.ReactNode;
  description?: React.ReactNode;
  initialPaths?: string[];
  allowEmpty?: boolean;
  confirmText?: React.ReactNode;
  confirmVariant?: React.ComponentProps<typeof Button>["variant"];
  /** Extra content rendered below the folder picker (e.g. per-run overrides). */
  extra?: React.ReactNode;
}

export function DirectorySelectionDialog({
  open,
  onOpenChange,
  onConfirm,
  title,
  description,
  initialPaths = [],
  allowEmpty = false,
  confirmText,
  confirmVariant = "default",
  extra,
}: IProps) {
  const intl = useIntl();
  const { configuration } = useConfigurationContext();
  const libraryPaths = configuration.general.stashes.map((s) => s.path);

  const [paths, setPaths] = useState<string[]>(initialPaths);
  const [currentDirectory, setCurrentDirectory] = useState("");

  function add() {
    const v = currentDirectory.trim();
    if (v && !paths.includes(v)) {
      setPaths(paths.concat(v));
      setCurrentDirectory("");
    }
  }

  function remove(p: string) {
    setPaths(paths.filter((path) => path !== p));
  }

  function reset() {
    setPaths(initialPaths);
    setCurrentDirectory("");
  }

  function handleOpenChange(o: boolean) {
    if (!o) reset();
    onOpenChange(o);
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="min-w-0 overflow-x-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {title ?? (
              <FormattedMessage
                id="actions.select_folders"
                defaultMessage="Select folders"
              />
            )}
          </DialogTitle>
        </DialogHeader>

        {description && <div className="min-w-0 text-sm">{description}</div>}

        <div className="min-w-0 space-y-3">
          {paths.length > 0 && (
            <ul className="w-full min-w-0 space-y-1">
              {paths.map((p) => (
                <li
                  key={p}
                  className="flex min-w-0 items-center justify-between gap-2 rounded-md border bg-card px-3 py-1.5 text-sm"
                >
                  <span
                    className="min-w-0 flex-1 truncate"
                    title={p}
                    data-selectable-text
                  >
                    {p}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => remove(p)}
                    aria-label={intl.formatMessage({
                      id: "actions.delete",
                      defaultMessage: "Delete",
                    })}
                  >
                    <Minus className="size-4" />
                  </Button>
                </li>
              ))}
            </ul>
          )}

          <FolderSelect
            currentDirectory={currentDirectory}
            onChangeDirectory={setCurrentDirectory}
            defaultDirectories={libraryPaths}
            appendButton={
              <Button
                type="button"
                variant="secondary"
                onClick={add}
                disabled={!currentDirectory.trim()}
                aria-label={intl.formatMessage({
                  id: "actions.add",
                  defaultMessage: "Add",
                })}
              >
                <Plus className="size-4" />
              </Button>
            }
          />

          {extra}
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
            variant={confirmVariant}
            disabled={!allowEmpty && paths.length === 0}
            onClick={() => {
              onConfirm(paths);
              reset();
            }}
          >
            {confirmText ?? (
              <FormattedMessage id="actions.confirm" defaultMessage="Confirm" />
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
