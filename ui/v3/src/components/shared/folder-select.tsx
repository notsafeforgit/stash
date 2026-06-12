import { useEffect, useRef, useState } from "react";
import { useIntl } from "react-intl";
import { useQuery } from "@apollo/client/react";
import { CornerLeftUp, Folder, MoreHorizontal, X } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Input } from "src/components/ui/input";
import { Button } from "src/components/ui/button";
import { ScrollArea } from "src/components/ui/scroll-area";
import { Spinner } from "src/components/ui/spinner";
import { useDebouncedValue } from "src/hooks/debounce";
import { cn } from "src/lib/utils";

/**
 * Last path component, treating both / and \ as separators so this works on
 * Windows-hosted stashes too. Falls back to the full path when there is no
 * separator (e.g. a root-level default like `D:`).
 */
function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

interface IProps {
  currentDirectory: string;
  onChangeDirectory: (value: string) => void;
  defaultDirectories?: string[];
  appendButton?: React.ReactNode;
  collapsible?: boolean;
  hideError?: boolean;
  className?: string;
}

export function FolderSelect({
  currentDirectory,
  onChangeDirectory,
  defaultDirectories = [],
  appendButton,
  collapsible = false,
  hideError = false,
  className,
}: IProps) {
  const intl = useIntl();
  const [showBrowser, setShowBrowser] = useState(!collapsible);
  const debouncedPath = useDebouncedValue(currentDirectory, 250);

  // Skip the empty-path query: the server interprets "" as the user's home
  // directory and returns its contents, which would briefly flash through
  // the cache as soon as `currentDirectory` first turns truthy.
  const { data, loading, error } = useQuery(GQL.DirectoryDocument, {
    variables: { path: debouncedPath },
    skip: !debouncedPath,
  });
  // Keep the latest non-loading result so the list doesn't flicker while
  // the user types.
  const prevData = useRef<typeof data | undefined>(undefined);
  if (!loading && data) prevData.current = data;
  const currentData = loading ? prevData.current : data;

  useEffect(() => {
    if (!collapsible) setShowBrowser(true);
  }, [collapsible]);

  // Only trust cached data whose echoed `path` matches the user's current
  // intent. During the debounce gap (or while the new query is in flight)
  // this hides stale results from the previous directory rather than
  // showing them as if they belonged to the just-tapped one.
  const dataMatchesPath = currentData?.directory.path === currentDirectory;
  const directories =
    !dataMatchesPath || (error && hideError)
      ? []
      : (currentData?.directory.directories ?? []);
  const parent = dataMatchesPath ? currentData?.directory.parent : undefined;

  const selectableDirectories =
    currentDirectory && directories.length > 0
      ? directories
      : currentDirectory
        ? []
        : defaultDirectories;

  function setInstant(value: string) {
    onChangeDirectory(value);
  }

  function goUp() {
    if (defaultDirectories.includes(currentDirectory)) {
      setInstant("");
    } else if (parent) {
      setInstant(parent);
    }
  }

  return (
    <div className={cn("space-y-2", className)}>
      <div className="flex items-stretch gap-2">
        <div className="flex flex-1 items-center gap-1.5">
          <Input
            placeholder={intl.formatMessage({
              id: "setup.folder.file_path",
              defaultMessage: "Folder path",
            })}
            value={currentDirectory}
            onChange={(e) => onChangeDirectory(e.currentTarget.value)}
            spellCheck={false}
            autoComplete="off"
          />
          {/* Always-rendered slot so the input width doesn't jiggle as the
              spinner/error icon toggles on each directory load. */}
          <span className="flex size-4 shrink-0 items-center justify-center">
            {loading && <Spinner className="size-4" />}
            {!loading && !hideError && error && (
              <X className="size-4 text-destructive" />
            )}
          </span>
        </div>
        {appendButton}
        {collapsible && (
          <Button
            type="button"
            variant="outline"
            size="icon"
            onClick={() => setShowBrowser((v) => !v)}
            aria-label={
              showBrowser
                ? intl.formatMessage({
                    id: "actions.hide",
                    defaultMessage: "Hide",
                  })
                : intl.formatMessage({
                    id: "actions.browse",
                    defaultMessage: "Browse",
                  })
            }
          >
            <MoreHorizontal className="size-4" />
          </Button>
        )}
      </div>

      {!hideError && error && (
        <div className="text-sm text-destructive">{error.message}</div>
      )}

      {/* Fixed height (not max-h) so the surrounding dialog doesn't resize
          and re-centre on every directory navigation. */}
      {showBrowser && (
        <ScrollArea
          className="rounded-md border bg-card"
          viewportClassName="h-64"
        >
          <ul className="w-full min-w-0">
            {currentDirectory && parent && (
              <li>
                <Button
                  type="button"
                  variant="ghost"
                  onClick={goUp}
                  disabled={loading}
                  className="h-auto w-full justify-start rounded-none px-3 py-1.5 text-sm font-normal"
                >
                  <CornerLeftUp className="size-4 text-muted-foreground" />
                  <span>
                    {intl.formatMessage({
                      id: "setup.folder.up_dir",
                      defaultMessage: "Up a directory",
                    })}
                  </span>
                </Button>
              </li>
            )}
            {selectableDirectories.length === 0 && !loading && (
              <li className="px-3 py-2 text-sm text-muted-foreground">
                {currentDirectory
                  ? intl.formatMessage({
                      id: "setup.folder.empty",
                      defaultMessage: "No directories.",
                    })
                  : intl.formatMessage({
                      id: "setup.folder.choose",
                      defaultMessage: "Type a path or choose a library folder.",
                    })}
              </li>
            )}
            {selectableDirectories.map((dir) => {
              // When we're listing children of a currentDirectory, the parent
              // prefix is redundant (it's already shown in the input above)
              // and truncating from the right hides the distinguishing tail —
              // show just the basename. For root-level library defaults the
              // full path is what disambiguates between multiple libraries.
              const label = currentDirectory ? basename(dir) : dir;
              return (
                <li key={dir} className="min-w-0">
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setInstant(dir)}
                    disabled={loading}
                    title={dir}
                    className="h-auto w-full min-w-0 justify-start rounded-none px-3 py-1.5 text-sm font-normal"
                  >
                    <Folder className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate text-left">
                      {label}
                    </span>
                  </Button>
                </li>
              );
            })}
          </ul>
        </ScrollArea>
      )}
    </div>
  );
}
