import { useEffect, useMemo, useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { useMutation, useQuery } from "@apollo/client/react";
import {
  ChevronDown,
  ChevronUp,
  Cog,
  FolderOpen,
  Minus,
  Plus,
  Settings,
} from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
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
import { useConfigurationContext } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { withoutTypename } from "src/utils/data";
import { DirectorySelectionDialog } from "src/components/shared/directory-selection-dialog";
import {
  AUTOTAG_SCRAPER_ID,
  getDefaultOptions,
  SCRAPER_PREFIX,
  STASH_BOX_PREFIX,
  type IScraperSource,
} from "./identify-types";
import { IdentifyOptionsEditor } from "./identify-options-editor";
import { IdentifySourceEditor } from "./identify-source-editor";

interface IProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Optional preselected scene IDs (from bulk-action). */
  selectedIds?: string[];
}

export function IdentifyDialog({ open, onOpenChange, selectedIds }: IProps) {
  const intl = useIntl();
  const toast = useToast();
  const { configuration } = useConfigurationContext();
  const { data: scraperData } = useQuery(GQL.ListSceneScrapersDocument);

  const [identify] = useMutation(GQL.MetadataIdentifyDocument);
  const [configureDefaults] = useMutation(GQL.ConfigureDefaultsDocument);

  const [options, setOptions] = useState<GQL.IdentifyMetadataOptionsInput>(
    getDefaultOptions(),
  );
  const [sources, setSources] = useState<IScraperSource[]>([]);
  const [paths, setPaths] = useState<string[]>([]);
  const [savingDefaults, setSavingDefaults] = useState(false);
  const [running, setRunning] = useState(false);
  const [pathsOpen, setPathsOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [editingSource, setEditingSource] = useState<
    IScraperSource | undefined
  >();

  // Build the master list of selectable scrapers from config (stash-boxes)
  // and from the listScrapers query (fragment-scrape-supporting scrapers).
  const allSources = useMemo<IScraperSource[]>(() => {
    const ret: IScraperSource[] = [];
    ret.push(
      ...configuration.general.stashBoxes.map((b, i) => ({
        id: `${STASH_BOX_PREFIX}${i}`,
        displayName: `stash-box: ${b.name}`,
        stash_box_endpoint: b.endpoint,
      })),
    );
    const fragmentScrapers = (scraperData?.listScrapers ?? []).filter((s) =>
      s.scene?.supported_scrapes.includes(GQL.ScrapeType.Fragment),
    );
    ret.push(
      ...fragmentScrapers.map((s) => ({
        id: `${SCRAPER_PREFIX}${s.id}`,
        displayName: s.name,
        scraper_id: s.id,
      })),
    );
    return ret;
  }, [configuration.general.stashBoxes, scraperData]);

  // Seed once from configuration.defaults.identify; fall back to first
  // stash-box + auto-tag scraper with conservative defaults.
  const [seeded, setSeeded] = useState(false);
  useEffect(() => {
    if (seeded || !allSources.length) return;
    const id = configuration.defaults.identify;
    if (id) {
      const mappedSources = id.sources
        .map((s) => {
          const found = allSources.find(
            (ss) =>
              ss.scraper_id === s.source.scraper_id ||
              ss.stash_box_endpoint === s.source.stash_box_endpoint,
          );
          if (!found) return null;
          const out: IScraperSource = { ...found };
          if (s.options) {
            const sopts = withoutTypename(s.options);
            sopts.fieldOptions = sopts.fieldOptions?.map(withoutTypename);
            out.options = sopts;
          }
          return out;
        })
        .filter((s): s is IScraperSource => s !== null);
      setSources(mappedSources);
      if (id.options) {
        const defaults = withoutTypename(id.options);
        defaults.fieldOptions = defaults.fieldOptions?.map(withoutTypename);
        setOptions(defaults);
      }
    } else {
      const stashBox = allSources.find((s) => s.stash_box_endpoint);
      const autoTag = allSources.find(
        (s) => s.id === `${SCRAPER_PREFIX}${AUTOTAG_SCRAPER_ID}`,
      );
      const next: IScraperSource[] = [];
      if (stashBox) next.push(stashBox);
      if (autoTag) {
        next.push({
          ...autoTag,
          options: {
            setOrganized: false,
            skipMultipleMatches: true,
            skipSingleNamePerformers: true,
          },
        });
      }
      setSources(next);
    }
    setSeeded(true);
  }, [allSources, configuration.defaults.identify, seeded]);

  // Add button is enabled when there are unselected sources left.
  const availableForAdd = useMemo(
    () => allSources.filter((s) => !sources.some((ss) => ss.id === s.id)),
    [allSources, sources],
  );

  function moveSource(index: number, dir: -1 | 1) {
    const target = index + dir;
    if (target < 0 || target >= sources.length) return;
    const next = [...sources];
    [next[index], next[target]] = [next[target], next[index]];
    setSources(next);
  }

  function removeSource(index: number) {
    setSources(sources.filter((_, i) => i !== index));
  }

  function makeIdentifyInput(): GQL.IdentifyMetadataInput {
    return {
      sources: sources.map((s) => ({
        source: {
          scraper_id: s.scraper_id,
          stash_box_endpoint: s.stash_box_endpoint,
        },
        options: s.options,
      })),
      options,
      sceneIDs: selectedIds,
      paths: paths.length ? paths : undefined,
    };
  }

  async function onIdentify() {
    setRunning(true);
    try {
      await identify({ variables: { input: makeIdentifyInput() } });
      toast.success(
        intl.formatMessage(
          {
            id: "config.tasks.added_job_to_queue",
            defaultMessage: "Added {operation_name} job to queue.",
          },
          {
            operation_name: intl.formatMessage({
              id: "actions.identify",
              defaultMessage: "Identify",
            }),
          },
        ),
      );
      onOpenChange(false);
    } catch (e) {
      toast.error(e);
    } finally {
      setRunning(false);
    }
  }

  async function setAsDefault() {
    setSavingDefaults(true);
    try {
      const input = makeIdentifyInput();
      const { sceneIDs: _s, paths: _p, ...rest } = input;
      await configureDefaults({ variables: { input: { identify: rest } } });
      toast.success(
        intl.formatMessage(
          {
            id: "config.tasks.defaults_set",
            defaultMessage: "{action} defaults saved.",
          },
          {
            action: intl.formatMessage({
              id: "actions.identify",
              defaultMessage: "Identify",
            }),
          },
        ),
      );
    } catch (e) {
      toast.error(e);
    } finally {
      setSavingDefaults(false);
    }
  }

  const selectionSummary = selectedIds ? (
    <FormattedMessage
      id="config.tasks.identify.identifying_scenes"
      defaultMessage="Identifying {num} {scene}"
      values={{
        num: selectedIds.length,
        scene: intl.formatMessage(
          { id: "countables.scenes", defaultMessage: "scene(s)" },
          { count: selectedIds.length },
        ),
      }}
    />
  ) : paths.length ? (
    <>
      <FormattedMessage
        id="config.tasks.identify.identifying_from_paths"
        defaultMessage="Identifying scenes from"
      />
      <ul className="ml-2 list-disc pl-3">
        {paths.map((p) => (
          <li key={p}>{p}</li>
        ))}
      </ul>
    </>
  ) : (
    <FormattedMessage
      id="config.tasks.identify.identifying_scenes"
      defaultMessage="Identifying {num} {scene}"
      values={{
        num: intl.formatMessage({ id: "all", defaultMessage: "all" }),
        scene: intl.formatMessage(
          { id: "countables.scenes", defaultMessage: "scene(s)" },
          { count: 0 },
        ),
      }}
    />
  );

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Cog className="size-4 text-muted-foreground" />
              <FormattedMessage
                id="actions.identify"
                defaultMessage="Identify"
              />
            </DialogTitle>
            <DialogDescription>
              <FormattedMessage
                id="config.tasks.identify.description"
                defaultMessage="Automatically set scene metadata using stash-box and scraper sources."
              />
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-5">
            <div className="flex items-center justify-between gap-3 rounded-md border bg-card px-3 py-2 text-sm">
              <div>{selectionSummary}</div>
              {!selectedIds && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setPathsOpen(true)}
                >
                  <FolderOpen className="size-4" />
                  <FormattedMessage
                    id="actions.select_folders"
                    defaultMessage="Select folders"
                  />
                </Button>
              )}
            </div>

            <section className="space-y-2">
              <h3 className="text-sm font-semibold">
                <FormattedMessage
                  id="config.tasks.identify.sources"
                  defaultMessage="Sources"
                />
              </h3>
              <ul className="space-y-1">
                {sources.length === 0 && (
                  <li className="rounded-md border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
                    <FormattedMessage
                      id="config.tasks.identify.no_sources"
                      defaultMessage="Add a source to begin."
                    />
                  </li>
                )}
                {sources.map((s, i) => (
                  <li
                    key={s.id}
                    className="flex items-center justify-between gap-2 rounded-md border bg-card px-3 py-1.5 text-sm"
                  >
                    <div className="flex items-center gap-2">
                      <div className="flex flex-col">
                        <Button
                          type="button"
                          size="icon-sm"
                          variant="ghost"
                          disabled={i === 0}
                          onClick={() => moveSource(i, -1)}
                          aria-label="Move up"
                        >
                          <ChevronUp className="size-3" />
                        </Button>
                        <Button
                          type="button"
                          size="icon-sm"
                          variant="ghost"
                          disabled={i === sources.length - 1}
                          onClick={() => moveSource(i, 1)}
                          aria-label="Move down"
                        >
                          <ChevronDown className="size-3" />
                        </Button>
                      </div>
                      <span>{s.displayName}</span>
                    </div>
                    <div className="flex gap-1">
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        onClick={() => setEditingSource(s)}
                        aria-label="Edit source options"
                      >
                        <Settings className="size-4" />
                      </Button>
                      <Button
                        type="button"
                        size="icon-sm"
                        variant="ghost"
                        onClick={() => removeSource(i)}
                        aria-label="Remove source"
                      >
                        <Minus className="size-4 text-destructive" />
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
              {availableForAdd.length > 0 && (
                <div className="flex justify-end">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => setAddOpen(true)}
                  >
                    <Plus className="size-4" />
                    <FormattedMessage id="actions.add" defaultMessage="Add" />
                  </Button>
                </div>
              )}
            </section>

            <IdentifyOptionsEditor options={options} setOptions={setOptions} />
          </div>

          <DialogFooter className="sm:justify-between">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={savingDefaults || sources.length === 0}
              onClick={() => void setAsDefault()}
            >
              {savingDefaults && <Spinner className="size-4" />}
              <FormattedMessage
                id="actions.set_as_default"
                defaultMessage="Set as default"
              />
            </Button>
            <div className="flex gap-2">
              <DialogClose
                render={
                  <Button type="button" variant="outline">
                    <FormattedMessage
                      id="actions.cancel"
                      defaultMessage="Cancel"
                    />
                  </Button>
                }
              />
              <Button
                type="button"
                disabled={running || sources.length === 0}
                onClick={() => void onIdentify()}
              >
                {running && <Spinner className="size-4" />}
                <FormattedMessage
                  id="actions.identify"
                  defaultMessage="Identify"
                />
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <DirectorySelectionDialog
        open={pathsOpen}
        onOpenChange={setPathsOpen}
        title={
          <FormattedMessage
            id="actions.select_folders"
            defaultMessage="Select folders"
          />
        }
        initialPaths={paths}
        allowEmpty
        onConfirm={(p) => {
          setPaths(p);
          setPathsOpen(false);
        }}
      />

      <IdentifySourceEditor
        open={addOpen}
        onOpenChange={setAddOpen}
        availableSources={availableForAdd}
        defaultOptions={options}
        onSave={(s) => setSources([...sources, s])}
      />

      <IdentifySourceEditor
        open={editingSource !== undefined}
        onOpenChange={(o) => !o && setEditingSource(undefined)}
        source={editingSource}
        availableSources={availableForAdd}
        defaultOptions={options}
        onSave={(s) =>
          setSources(sources.map((ss) => (ss.id === s.id ? s : ss)))
        }
      />
    </>
  );
}
