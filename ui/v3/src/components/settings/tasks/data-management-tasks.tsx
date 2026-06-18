import { useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import { useToast } from "src/hooks/toast";
import { useConfigurationContext } from "src/hooks/config";
import { useTaskOptions } from "src/hooks/use-task-options";
import downloadFile from "src/utils/download";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "src/components/ui/radio-group";
import { Label } from "src/components/ui/label";
import { AlertTriangle, Database } from "lucide-react";
import { DestructiveConfirmDialog } from "src/components/shared/destructive-confirm-dialog";
import { ImportDialog } from "./import-dialog";
import { SelectivePathsButton } from "./selective-paths-button";
import {
  TaskGroup,
  TaskOptionToggle,
  TaskSectionHeading,
} from "./task-section";

function CleanOptionsForm({
  options,
  setOptions,
}: {
  options: GQL.CleanMetadataInput;
  setOptions: (s: GQL.CleanMetadataInput) => void;
}) {
  function set(input: Partial<GQL.CleanMetadataInput>) {
    setOptions({ ...options, ...input });
  }
  return (
    <>
      <TaskOptionToggle
        id="clean-ignore-zip-contents"
        label="Ignore zip file contents"
        description="Faster but will miss files removed inside zip files."
        checked={options.ignoreZipFileContents ?? false}
        onChange={(v) => set({ ignoreZipFileContents: v })}
      />
      <TaskOptionToggle
        id="clean-dry-run"
        label="Dry run"
        description="Don't remove anything."
        checked={options.dryRun}
        onChange={(v) => set({ dryRun: v })}
      />
    </>
  );
}

function CleanGeneratedOptionsForm({
  options,
  setOptions,
}: {
  options: GQL.CleanGeneratedInput;
  setOptions: (s: GQL.CleanGeneratedInput) => void;
}) {
  function set(input: Partial<GQL.CleanGeneratedInput>) {
    setOptions({ ...options, ...input });
  }
  return (
    <>
      <TaskOptionToggle
        id="cg-blob-files"
        label="Blob files"
        checked={options.blobFiles ?? false}
        onChange={(v) => set({ blobFiles: v })}
      />
      <TaskOptionToggle
        id="cg-screenshots"
        label="Scene previews"
        description="Scene previews and thumbnails"
        checked={options.screenshots ?? false}
        onChange={(v) => set({ screenshots: v })}
      />
      <TaskOptionToggle
        id="cg-sprites"
        label="Scene sprites"
        checked={options.sprites ?? false}
        onChange={(v) => set({ sprites: v })}
      />
      <TaskOptionToggle
        id="cg-transcodes"
        label="Scene transcodes"
        checked={options.transcodes ?? false}
        onChange={(v) => set({ transcodes: v })}
      />
      <TaskOptionToggle
        id="cg-markers"
        label="Marker previews"
        checked={options.markers ?? false}
        onChange={(v) => set({ markers: v })}
      />
      <TaskOptionToggle
        id="cg-image-thumbnails"
        label="Image thumbnails"
        description="Image thumbnails and clips"
        checked={options.imageThumbnails ?? false}
        onChange={(v) => set({ imageThumbnails: v })}
      />
      <TaskOptionToggle
        id="cg-dry-run"
        label="Dry run"
        checked={options.dryRun ?? false}
        onChange={(v) => set({ dryRun: v })}
      />
    </>
  );
}

function BackupDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (download: boolean, includeBlobs: boolean) => void;
}) {
  const intl = useIntl();
  const { configuration } = useConfigurationContext();
  // When blob storage is in the database (not on the filesystem), the
  // backup always includes blobs implicitly — surfaced as a disabled,
  // checked toggle.
  const blobsOnFS =
    configuration.general.blobsStorage === GQL.BlobsStorageType.Filesystem;
  const includeBlobsDefault = blobsOnFS;
  const [download, setDownload] = useState(false);
  const [includeBlobs, setIncludeBlobs] = useState(includeBlobsDefault);

  const backupDir =
    configuration.general.backupDirectoryPath ||
    `<${intl.formatMessage({
      id: "config.general.backup_directory_path.heading",
      defaultMessage: "Backup directory path",
    })}>`;

  const filenameFormat = !includeBlobs
    ? "[origFilename].sqlite.[schemaVersion].[YYYYMMDD_HHMMSS]"
    : "[origFilename].sqlite.[schemaVersion].[YYYYMMDD_HHMMSS].zip";

  const showBlobsWarning = blobsOnFS && includeBlobs !== includeBlobsDefault;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Database className="size-4 text-muted-foreground" />
            <FormattedMessage id="actions.backup" defaultMessage="Backup" />
          </DialogTitle>
          <DialogDescription>
            {intl.formatMessage({
              id: "config.tasks.backup_database.description",
              defaultMessage: "Back up the SQLite database.",
            })}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">
              <FormattedMessage
                id="config.tasks.backup_database.destination"
                defaultMessage="Destination"
              />
            </legend>
            <RadioGroup
              value={download ? "download" : "directory"}
              onValueChange={(v) => setDownload(v === "download")}
            >
              <div className="flex items-start gap-2">
                <RadioGroupItem
                  id="backup-dest-directory"
                  value="directory"
                  className="mt-0.5"
                />
                <Label
                  htmlFor="backup-dest-directory"
                  className="cursor-pointer leading-snug font-normal"
                >
                  <FormattedMessage
                    id="config.tasks.backup_database.to_directory"
                    defaultMessage="Save to backup directory ({directory})"
                    values={{
                      directory: (
                        <code className="rounded bg-muted px-1 py-0.5 text-xs">
                          {backupDir}
                        </code>
                      ),
                    }}
                  />
                </Label>
              </div>
              <div className="flex items-start gap-2">
                <RadioGroupItem
                  id="backup-dest-download"
                  value="download"
                  className="mt-0.5"
                />
                <Label
                  htmlFor="backup-dest-download"
                  className="cursor-pointer leading-snug font-normal"
                >
                  <FormattedMessage
                    id="config.tasks.backup_database.download"
                    defaultMessage="Download to this device"
                  />
                </Label>
              </div>
            </RadioGroup>
          </fieldset>

          <TaskOptionToggle
            id="backup-include-blobs"
            label={intl.formatMessage({
              id: "config.tasks.backup_database.include_blobs",
              defaultMessage: "Include blob files",
            })}
            description={
              blobsOnFS
                ? intl.formatMessage({
                    id: "config.tasks.backup_database.include_blobs_desc",
                    defaultMessage:
                      "Include blob storage (covers, performer/studio/tag images) in the backup. Greatly increases backup size and the time taken to produce it, but gives a complete restore-without-media-files snapshot.",
                  })
                : intl.formatMessage({
                    id: "config.tasks.backup_database.blobs_in_db",
                    defaultMessage:
                      "Blob storage is in the database — included automatically.",
                  })
            }
            checked={includeBlobs || !blobsOnFS}
            disabled={!blobsOnFS}
            onChange={setIncludeBlobs}
          />

          {showBlobsWarning && (
            <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
              <AlertTriangle className="mt-0.5 size-4 shrink-0" />
              <span>
                <FormattedMessage
                  id="config.tasks.backup_database.warning_blobs"
                  defaultMessage="You've changed the default for blob inclusion. A backup without blobs is only restorable on a system whose blob storage is still intact."
                />
              </span>
            </div>
          )}

          <p className="text-xs text-muted-foreground">
            <FormattedMessage
              id={
                !includeBlobs
                  ? "config.tasks.backup_database.sqlite"
                  : "config.tasks.backup_database.zip"
              }
              defaultMessage="The backup will be named {filename_format}."
              values={{
                filename_format: (
                  <code className="rounded bg-muted px-1 py-0.5 text-xs">
                    {filenameFormat}
                  </code>
                ),
              }}
            />
          </p>
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
            onClick={() => onConfirm(download, includeBlobs)}
          >
            {download
              ? intl.formatMessage({
                  id: "config.tasks.backup_database.download",
                  defaultMessage: "Download",
                })
              : intl.formatMessage({
                  id: "actions.backup",
                  defaultMessage: "Backup",
                })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CleanGeneratedDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (opts: GQL.CleanGeneratedInput) => void;
}) {
  const [options, update] = useTaskOptions("cleanGenerated", () => ({
    blobFiles: true,
    imageThumbnails: true,
    markers: true,
    screenshots: true,
    sprites: true,
    transcodes: true,
    dryRun: false,
  }));
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            <FormattedMessage
              id="actions.clean_generated"
              defaultMessage="Clean generated files"
            />
          </DialogTitle>
          <DialogDescription>
            Removes generated files without a corresponding database entry.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <CleanGeneratedOptionsForm options={options} setOptions={update} />
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
            variant="destructive"
            onClick={() => onConfirm(options)}
          >
            <FormattedMessage
              id="actions.clean_generated"
              defaultMessage="Clean"
            />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function DataManagementTasks() {
  const intl = useIntl();
  const toast = useToast();

  const [cleanOptions, setCleanOptions] = useTaskOptions("clean", () => ({
    dryRun: false,
  }));
  const [migrateBlobsOpts, setMigrateBlobsOpts] =
    useState<GQL.MigrateBlobsInput>({
      deleteOld: true,
    });
  const [migrateScreenshotsOpts, setMigrateScreenshotsOpts] =
    useState<GQL.MigrateSceneScreenshotsInput>({
      deleteFiles: false,
      overwriteExisting: false,
    });

  const [cleanAlertOpen, setCleanAlertOpen] = useState(false);
  const [cleanGenOpen, setCleanGenOpen] = useState(false);
  const [backupOpen, setBackupOpen] = useState(false);
  const [importAlertOpen, setImportAlertOpen] = useState(false);
  const [importFileOpen, setImportFileOpen] = useState(false);

  const [clean] = useMutation(GQL.MetadataCleanDocument);
  const [cleanGenerated] = useMutation(GQL.MetadataCleanGeneratedDocument);
  const [optimise] = useMutation(GQL.OptimiseDatabaseDocument);
  const [exportData] = useMutation(GQL.MetadataExportDocument);
  const [importData] = useMutation(GQL.MetadataImportDocument);
  const [backup] = useMutation(GQL.BackupDatabaseDocument);
  const [anonymise] = useMutation(GQL.AnonymiseDatabaseDocument);
  const [migrateHash] = useMutation(GQL.MigrateHashNamingDocument);
  const [migrateBlobs] = useMutation(GQL.MigrateBlobsDocument);
  const [migrateScreenshots] = useMutation(GQL.MigrateSceneScreenshotsDocument);
  const [migrateLegacySavedFilters] = useMutation(
    GQL.MigrateLegacySavedFiltersDocument,
  );

  function added(op: string) {
    toast.success(
      intl.formatMessage(
        {
          id: "config.tasks.added_job_to_queue",
          defaultMessage: "Added {operation_name} job to queue.",
        },
        { operation_name: op },
      ),
    );
  }

  async function tryRun(label: string, fn: () => Promise<unknown>) {
    try {
      await fn();
      added(label);
    } catch (e) {
      toast.error(e);
    }
  }

  async function onClean(paths?: string[]) {
    setCleanAlertOpen(false);
    await tryRun(
      intl.formatMessage({ id: "actions.clean", defaultMessage: "Clean" }),
      () => clean({ variables: { input: { ...cleanOptions, paths } } }),
    );
  }

  async function onCleanGenerated(input: GQL.CleanGeneratedInput) {
    setCleanGenOpen(false);
    await tryRun(
      intl.formatMessage({
        id: "actions.clean_generated",
        defaultMessage: "Clean generated",
      }),
      () => cleanGenerated({ variables: { input } }),
    );
  }

  // Backup and Anonymise are synchronous server operations (the
  // database is dumped in-thread before the mutation returns), not
  // queued jobs. They can run for tens of seconds on large libraries —
  // toast.promise gives the user a stable "in progress" indicator that
  // turns into success / error when the response lands.
  async function onBackup(download: boolean, includeBlobs: boolean) {
    setBackupOpen(false);
    try {
      await toast.promise(
        backup({ variables: { input: { download, includeBlobs } } }),
        {
          loading: intl.formatMessage({
            id: "config.tasks.backing_up_database",
            defaultMessage: "Backing up database…",
          }),
          success: (res) => {
            if (download && res.data?.backupDatabase) {
              downloadFile(res.data.backupDatabase);
              return intl.formatMessage({
                id: "config.tasks.backup_database.downloaded",
                defaultMessage: "Backup downloaded.",
              });
            }
            return intl.formatMessage({
              id: "config.tasks.backup_database.complete",
              defaultMessage: "Backup complete.",
            });
          },
        },
      );
    } catch {
      /* toast.promise already surfaced the error */
    }
  }

  async function onAnonymise(download: boolean) {
    try {
      await toast.promise(anonymise({ variables: { input: { download } } }), {
        loading: intl.formatMessage({
          id: "config.tasks.anonymising_database",
          defaultMessage: "Anonymising database…",
        }),
        success: (res) => {
          if (download && res.data?.anonymiseDatabase) {
            downloadFile(res.data.anonymiseDatabase);
            return intl.formatMessage({
              id: "config.tasks.anonymise_database.downloaded",
              defaultMessage: "Anonymised copy downloaded.",
            });
          }
          return intl.formatMessage({
            id: "config.tasks.anonymise_database.complete",
            defaultMessage: "Anonymised copy written.",
          });
        },
      });
    } catch {
      /* toast.promise already surfaced the error */
    }
  }

  async function onImport() {
    setImportAlertOpen(false);
    await tryRun(
      intl.formatMessage({ id: "actions.import", defaultMessage: "Import" }),
      () => importData(),
    );
  }

  return (
    <>
      <TaskGroup
        title={intl.formatMessage({
          id: "config.tasks.maintenance",
          defaultMessage: "Maintenance",
        })}
      >
        <TaskSectionHeading
          title={<FormattedMessage id="actions.clean" defaultMessage="Clean" />}
          description={intl.formatMessage({
            id: "config.tasks.cleanup_desc",
            defaultMessage:
              "Check for missing files and remove them from the database. This is a destructive action.",
          })}
          actions={
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="destructive"
                onClick={() => setCleanAlertOpen(true)}
              >
                <FormattedMessage id="actions.clean" defaultMessage="Clean" />…
              </Button>
              <SelectivePathsButton
                buttonLabel={
                  <FormattedMessage
                    id="actions.selective_clean"
                    defaultMessage="Selective clean"
                  />
                }
                dialogTitle={
                  <FormattedMessage
                    id="actions.selective_clean"
                    defaultMessage="Selective clean"
                  />
                }
                dialogDescription={
                  cleanOptions.dryRun
                    ? "Dry mode selected — nothing will be removed."
                    : "Database entries for missing files inside the selected paths will be removed."
                }
                confirmVariant="destructive"
                confirmText={
                  <FormattedMessage id="actions.clean" defaultMessage="Clean" />
                }
                onConfirm={(paths) => void onClean(paths)}
              />
            </div>
          }
          collapsible
        >
          <CleanOptionsForm
            options={cleanOptions}
            setOptions={setCleanOptions}
          />
        </TaskSectionHeading>

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.clean_generated"
              defaultMessage="Clean generated files"
            />
          }
          description="Removes generated files without a corresponding database entry."
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() => setCleanGenOpen(true)}
            >
              <FormattedMessage
                id="actions.clean_generated"
                defaultMessage="Clean generated"
              />
              …
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.optimise_database"
              defaultMessage="Optimise database"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.optimise_database",
            defaultMessage:
              "Attempt to improve performance by analysing and rebuilding the database file.",
          })}
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.optimise_database",
                    defaultMessage: "Optimise database",
                  }),
                  () => optimise(),
                )
              }
            >
              <FormattedMessage
                id="actions.optimise_database"
                defaultMessage="Optimise database"
              />
            </Button>
          }
        />
      </TaskGroup>

      <TaskGroup
        title={intl.formatMessage({
          id: "metadata",
          defaultMessage: "Metadata",
        })}
      >
        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.full_export"
              defaultMessage="Full export"
            />
          }
          description="Exports the database content into JSON in the metadata directory."
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.export",
                    defaultMessage: "Export",
                  }),
                  () => exportData(),
                )
              }
            >
              <FormattedMessage
                id="actions.full_export"
                defaultMessage="Full export"
              />
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.full_import"
              defaultMessage="Full import"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.import_from_exported_json",
            defaultMessage:
              "Import from exported JSON in the metadata directory. Wipes the existing database.",
          })}
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() => setImportAlertOpen(true)}
            >
              <FormattedMessage
                id="actions.full_import"
                defaultMessage="Full import"
              />
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.import_from_file"
              defaultMessage="Import from file"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.incremental_import",
            defaultMessage:
              "Incremental import from a supplied export zip file.",
          })}
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() => setImportFileOpen(true)}
            >
              <FormattedMessage
                id="actions.import_from_file"
                defaultMessage="Import from file"
              />
              …
            </Button>
          }
        />
      </TaskGroup>

      <TaskGroup
        title={intl.formatMessage({
          id: "actions.backup",
          defaultMessage: "Backup",
        })}
      >
        <TaskSectionHeading
          title={
            <FormattedMessage id="actions.backup" defaultMessage="Backup" />
          }
          description="Back up the SQLite database."
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={() => setBackupOpen(true)}
            >
              <FormattedMessage id="actions.backup" defaultMessage="Backup" />…
            </Button>
          }
        />
        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.anonymise"
              defaultMessage="Anonymise"
            />
          }
          description="Produce an anonymised copy of the database for sharing."
          actions={
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="secondary"
                onClick={() => void onAnonymise(false)}
              >
                <FormattedMessage
                  id="actions.anonymise"
                  defaultMessage="Anonymise"
                />
              </Button>
              <Button
                type="button"
                variant="secondary"
                onClick={() => void onAnonymise(true)}
              >
                <FormattedMessage
                  id="actions.download_anonymised"
                  defaultMessage="Download anonymised"
                />
              </Button>
            </div>
          }
        />
      </TaskGroup>

      <TaskGroup
        title={intl.formatMessage({
          id: "config.tasks.migrations",
          defaultMessage: "Migrations",
        })}
      >
        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.rename_gen_files"
              defaultMessage="Rename generated files"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.migrate_hash_files",
            defaultMessage:
              "Used after changing the generated file naming hash.",
          })}
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.hash_migration",
                    defaultMessage: "Hash migration",
                  }),
                  () => migrateHash(),
                )
              }
            >
              <FormattedMessage
                id="actions.rename_gen_files"
                defaultMessage="Rename generated files"
              />
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.migrate_saved_filters"
              defaultMessage="Migrate saved filters"
            />
          }
          description={intl.formatMessage({
            id: "config.tasks.migrate_saved_filters.description",
            defaultMessage:
              "Convert legacy v2.5 saved filter criteria and default filters to the filter AST format.",
          })}
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.migrate_saved_filters",
                    defaultMessage: "Migrate saved filters",
                  }),
                  () => migrateLegacySavedFilters(),
                )
              }
            >
              <FormattedMessage
                id="actions.migrate_saved_filters"
                defaultMessage="Migrate saved filters"
              />
            </Button>
          }
        />

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.migrate_blobs"
              defaultMessage="Migrate blobs"
            />
          }
          description="Migrate blobs to the current blob storage system."
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.migrate_blobs",
                    defaultMessage: "Migrate blobs",
                  }),
                  () =>
                    migrateBlobs({ variables: { input: migrateBlobsOpts } }),
                )
              }
            >
              <FormattedMessage
                id="actions.migrate_blobs"
                defaultMessage="Migrate blobs"
              />
            </Button>
          }
          collapsible
        >
          <TaskOptionToggle
            id="migrate-blobs-delete-old"
            label="Delete old data after migration"
            checked={migrateBlobsOpts.deleteOld ?? false}
            onChange={(v) =>
              setMigrateBlobsOpts({ ...migrateBlobsOpts, deleteOld: v })
            }
          />
        </TaskSectionHeading>

        <TaskSectionHeading
          title={
            <FormattedMessage
              id="actions.migrate_scene_screenshots"
              defaultMessage="Migrate scene screenshots"
            />
          }
          description="Migrate scene screenshots into the new blob storage system."
          actions={
            <Button
              type="button"
              variant="destructive"
              onClick={() =>
                void tryRun(
                  intl.formatMessage({
                    id: "actions.migrate_scene_screenshots",
                    defaultMessage: "Migrate scene screenshots",
                  }),
                  () =>
                    migrateScreenshots({
                      variables: { input: migrateScreenshotsOpts },
                    }),
                )
              }
            >
              <FormattedMessage
                id="actions.migrate_scene_screenshots"
                defaultMessage="Migrate scene screenshots"
              />
            </Button>
          }
          collapsible
        >
          <TaskOptionToggle
            id="migrate-screenshots-overwrite"
            label="Overwrite existing blobs with screenshot data"
            checked={migrateScreenshotsOpts.overwriteExisting ?? false}
            onChange={(v) =>
              setMigrateScreenshotsOpts({
                ...migrateScreenshotsOpts,
                overwriteExisting: v,
              })
            }
          />
          <TaskOptionToggle
            id="migrate-screenshots-delete"
            label="Delete screenshot files after migration"
            checked={migrateScreenshotsOpts.deleteFiles ?? false}
            onChange={(v) =>
              setMigrateScreenshotsOpts({
                ...migrateScreenshotsOpts,
                deleteFiles: v,
              })
            }
          />
        </TaskSectionHeading>
      </TaskGroup>

      <DestructiveConfirmDialog
        open={cleanAlertOpen}
        onOpenChange={setCleanAlertOpen}
        title={<FormattedMessage id="actions.clean" defaultMessage="Clean" />}
        confirmText={
          <FormattedMessage id="actions.clean" defaultMessage="Clean" />
        }
        onConfirm={() => void onClean()}
      >
        <p className="text-sm">
          {cleanOptions.dryRun
            ? "Dry mode selected — nothing will be removed."
            : "This will remove database entries for missing files. This is destructive. Continue?"}
        </p>
      </DestructiveConfirmDialog>

      <CleanGeneratedDialog
        open={cleanGenOpen}
        onOpenChange={setCleanGenOpen}
        onConfirm={(o) => void onCleanGenerated(o)}
      />

      <BackupDialog
        open={backupOpen}
        onOpenChange={setBackupOpen}
        onConfirm={(download, includeBlobs) =>
          void onBackup(download, includeBlobs)
        }
      />

      <ImportDialog open={importFileOpen} onOpenChange={setImportFileOpen} />

      <DestructiveConfirmDialog
        open={importAlertOpen}
        onOpenChange={setImportAlertOpen}
        title={
          <FormattedMessage
            id="actions.full_import"
            defaultMessage="Full import"
          />
        }
        confirmText={
          <FormattedMessage id="actions.import" defaultMessage="Import" />
        }
        onConfirm={() => void onImport()}
      >
        <p className="text-sm">
          This will wipe the existing database and replace it with the contents
          of the metadata directory. This is destructive.
        </p>
      </DestructiveConfirmDialog>
    </>
  );
}
