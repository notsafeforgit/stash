import { useState } from "react";
import { FormattedMessage } from "react-intl";
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
  /** Called with the chosen directory when the user confirms. */
  onSelect: (path: string) => void;
  /** Seed the browser at this path (e.g. the setting's current value). */
  initialPath?: string;
  title?: React.ReactNode;
}

/**
 * Single-directory picker: browse with FolderSelect, confirm one path.
 * For multi-path selection (scan/auto-tag style) use
 * DirectorySelectionDialog instead.
 */
export function FolderPickerDialog({
  open,
  onOpenChange,
  onSelect,
  initialPath = "",
  title,
}: IProps) {
  const { configuration } = useConfigurationContext();
  const libraryPaths = configuration.general.stashes.map((s) => s.path);

  const [currentDirectory, setCurrentDirectory] = useState(initialPath);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="min-w-0 overflow-x-hidden sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {title ?? (
              <FormattedMessage
                id="setup.folder.choose"
                defaultMessage="Choose folder"
              />
            )}
          </DialogTitle>
        </DialogHeader>

        <FolderSelect
          currentDirectory={currentDirectory}
          onChangeDirectory={setCurrentDirectory}
          defaultDirectories={libraryPaths}
        />

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
            disabled={!currentDirectory.trim()}
            onClick={() => {
              onSelect(currentDirectory.trim());
              onOpenChange(false);
            }}
          >
            <FormattedMessage id="actions.confirm" defaultMessage="Confirm" />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
