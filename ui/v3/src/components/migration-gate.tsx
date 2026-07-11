import { useMutation, useQuery, useSubscription } from "@apollo/client/react";
import {
  AlertTriangleIcon,
  DatabaseZapIcon,
  RefreshCwIcon,
} from "lucide-react";
import {
  type PropsWithChildren,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { DEFAULT_LOCALE, LocaleProvider } from "@/components/locale-provider";
import { SetupWizard } from "@/components/setup-wizard";
import * as GQL from "@/core/generated-graphql";

type SystemStatus = GQL.SystemStatusQuery["systemStatus"];
type MigrationJob = Pick<
  GQL.Job,
  "id" | "status" | "subTasks" | "description" | "progress" | "error"
>;

function FullPageSpinner() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
      <Spinner className="size-10 text-muted-foreground" />
    </div>
  );
}

function backupTimestamp() {
  return new Date()
    .toISOString()
    .replace(/T/g, "_")
    .replace(/-/g, "")
    .replace(/:/g, "")
    .replace(/\..*/, "");
}

function defaultBackupPath(status: SystemStatus) {
  const databaseName = status.databasePath?.split(/[\\/]/).pop();
  if (
    !databaseName ||
    status.databaseSchema === undefined ||
    status.databaseSchema === null
  ) {
    return "";
  }
  return `${databaseName}.${status.databaseSchema}.${backupTimestamp()}`;
}

function isTerminalStatus(status: GQL.JobStatus) {
  return (
    status === GQL.JobStatus.Cancelled ||
    status === GQL.JobStatus.Failed ||
    status === GQL.JobStatus.Finished
  );
}

function useMigrationJob(
  jobID: string | null,
  onComplete: (job?: MigrationJob) => void,
) {
  const [job, setJob] = useState<MigrationJob>();
  const completedRef = useRef(false);

  useEffect(() => {
    if (!jobID) return;
    completedRef.current = false;
    setJob(undefined);
  }, [jobID]);

  const finish = useCallback(
    (finishedJob?: MigrationJob) => {
      if (completedRef.current) return;
      completedRef.current = true;
      setJob(undefined);
      onComplete(finishedJob);
    },
    [onComplete],
  );

  const { data: jobData, loading: jobLoading } = useQuery(GQL.FindJobDocument, {
    variables: { input: { id: jobID ?? "" } },
    skip: !jobID,
    fetchPolicy: "network-only",
    pollInterval: jobID ? 1000 : 0,
  });

  useEffect(() => {
    if (!jobID || jobLoading || !jobData) return;

    const foundJob = jobData.findJob;
    if (!foundJob) {
      finish();
      return;
    }

    setJob(foundJob);
    if (isTerminalStatus(foundJob.status)) {
      finish(foundJob);
    }
  }, [finish, jobData, jobID, jobLoading]);

  useSubscription(GQL.JobsLifecycleSubscribeDocument, {
    skip: !jobID,
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsLifecycleSubscribe;
      if (!event || event.job.id !== jobID) return;

      if (event.type === GQL.JobStatusUpdateType.Remove) {
        finish(event.job);
        return;
      }

      setJob(event.job);
    },
  });

  useSubscription(GQL.JobsProgressSubscribeDocument, {
    skip: !jobID,
    onData: ({ data: payload }) => {
      const event = payload.data?.jobsProgressSubscribe;
      if (!event || event.job.id !== jobID) return;

      setJob(event.job);
      if (isTerminalStatus(event.job.status)) {
        finish(event.job);
      }
    },
  });

  return job;
}

function MigrationRequiredDialog({
  status,
  onComplete,
}: {
  status: SystemStatus;
  onComplete: () => Promise<void> | void;
}) {
  const intl = useIntl();
  const computedBackupPath = useMemo(() => defaultBackupPath(status), [status]);
  const [backupPath, setBackupPath] = useState<string | undefined>();
  const [migrationError, setMigrationError] = useState("");
  const [jobID, setJobID] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [migrate] = useMutation(GQL.MigrateDocument);

  useEffect(() => {
    if (backupPath === undefined && computedBackupPath) {
      setBackupPath(computedBackupPath);
    }
  }, [backupPath, computedBackupPath]);

  const onJobComplete = useCallback(
    async (finishedJob?: MigrationJob) => {
      setJobID(null);
      setStarting(false);

      if (
        finishedJob?.error ||
        finishedJob?.status === GQL.JobStatus.Failed ||
        finishedJob?.status === GQL.JobStatus.Cancelled
      ) {
        setMigrationError(
          finishedJob.error ??
            `Migration job ended with status ${finishedJob.status}.`,
        );
        return;
      }

      await onComplete();
    },
    [onComplete],
  );

  const job = useMigrationJob(jobID, onJobComplete);
  const isMigrating = starting || jobID !== null;
  const progress =
    job?.progress !== undefined && job.progress !== null
      ? Math.round(job.progress * 100)
      : null;
  const isForkOnlyMigration =
    status.databaseSchema !== undefined &&
    status.databaseSchema !== null &&
    status.databaseSchema === status.appSchema;

  async function onMigrate() {
    setStarting(true);
    setMigrationError("");

    try {
      const result = await migrate({
        variables: { input: { backupPath: backupPath ?? "" } },
      });
      const nextJobID = result.data?.migrate;
      if (!nextJobID) {
        await onJobComplete();
        return;
      }
      setJobID(nextJobID);
    } catch (e) {
      setStarting(false);
      setJobID(null);
      setMigrationError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <>
      <FullPageSpinner />
      <Dialog open>
        <DialogContent
          className="max-h-[calc(100dvh-2rem)] gap-0 overflow-hidden p-0 sm:max-w-2xl"
          showCloseButton={false}
        >
          <DialogHeader className="border-b px-5 py-4">
            <div className="flex items-start gap-3">
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                <DatabaseZapIcon />
              </div>
              <div className="min-w-0 flex-1">
                <DialogTitle>
                  <FormattedMessage id="setup.migrate.migration_required" />
                </DialogTitle>
                <DialogDescription className="mt-1">
                  {isForkOnlyMigration ? (
                    <FormattedMessage id="setup.migrate.fork_schema_too_old" />
                  ) : (
                    <FormattedMessage
                      id="setup.migrate.schema_too_old"
                      values={{
                        databaseSchema: status.databaseSchema,
                        appSchema: status.appSchema,
                        strong: (chunks) => (
                          <strong className="font-medium text-foreground">
                            {chunks}
                          </strong>
                        ),
                        code: (chunks) => <code>{chunks}</code>,
                      }}
                    />
                  )}
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>

          <ScrollArea className="h-[min(32rem,calc(100dvh-12rem))]">
            <div className="flex flex-col gap-5 px-5 py-4">
              <p className="text-sm text-destructive">
                <AlertTriangleIcon
                  data-icon="inline-start"
                  className="mr-1 inline"
                />
                <FormattedMessage id="setup.migrate.migration_irreversible_warning" />
              </p>

              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="migration-backup-path">
                    <FormattedMessage id="setup.migrate.backup_database_path_leave_empty_to_disable_backup" />
                  </FieldLabel>
                  <Input
                    id="migration-backup-path"
                    value={backupPath ?? ""}
                    disabled={isMigrating}
                    placeholder={intl.formatMessage({
                      id: "setup.paths.database_filename_empty_for_default",
                    })}
                    onChange={(e) => setBackupPath(e.currentTarget.value)}
                  />
                  <FieldDescription>
                    <FormattedMessage
                      id="setup.migrate.backup_recommended"
                      values={{
                        defaultBackupPath: computedBackupPath,
                        code: (chunks) => <code>{chunks}</code>,
                      }}
                    />
                  </FieldDescription>
                </Field>
              </FieldGroup>

              {isMigrating && (
                <div className="flex flex-col gap-3">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Spinner data-icon="inline-start" />
                    <FormattedMessage id="setup.migrate.migrating_database" />
                  </div>
                  {progress !== null && (
                    <div className="flex items-center gap-3">
                      <Progress value={progress} className="flex-1" />
                      <span className="w-10 text-right text-sm tabular-nums text-muted-foreground">
                        {progress}%
                      </span>
                    </div>
                  )}
                  {job?.subTasks && job.subTasks.length > 0 && (
                    <ScrollArea className="h-36 rounded-lg bg-muted/50">
                      <div className="flex flex-col gap-1 p-3 text-xs text-muted-foreground">
                        {job.subTasks.map((subTask, index) => (
                          <div key={`${index}-${subTask}`}>{subTask}</div>
                        ))}
                      </div>
                    </ScrollArea>
                  )}
                </div>
              )}

              {migrationError && (
                <section className="flex flex-col gap-2" aria-live="polite">
                  <h2 className="text-sm font-medium text-destructive">
                    <FormattedMessage id="setup.migrate.migration_failed" />
                  </h2>
                  <p className="text-sm text-muted-foreground">
                    <FormattedMessage id="setup.migrate.migration_failed_error" />
                  </p>
                  <pre className="max-h-48 overflow-auto rounded-lg bg-muted p-3 text-xs whitespace-pre-wrap text-foreground">
                    {migrationError}
                  </pre>
                </section>
              )}
            </div>
          </ScrollArea>

          <DialogFooter>
            <Button type="button" onClick={onMigrate} disabled={isMigrating}>
              {isMigrating ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <RefreshCwIcon data-icon="inline-start" />
              )}
              <FormattedMessage id="setup.migrate.perform_schema_migration" />
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function SystemStatusGate({ children }: PropsWithChildren) {
  const { data, loading, refetch } = useQuery(GQL.SystemStatusDocument, {
    fetchPolicy: "network-only",
  });

  if (loading && !data) {
    return <FullPageSpinner />;
  }

  if (data?.systemStatus.status === GQL.SystemStatusEnum.NeedsMigration) {
    return (
      <LocaleProvider language={DEFAULT_LOCALE}>
        <MigrationRequiredDialog
          status={data.systemStatus}
          onComplete={async () => {
            await refetch();
          }}
        />
      </LocaleProvider>
    );
  }

  if (data?.systemStatus.status === GQL.SystemStatusEnum.Setup) {
    return (
      <LocaleProvider language={DEFAULT_LOCALE}>
        <SetupWizard
          status={data.systemStatus}
          onComplete={async () => {
            await refetch();
          }}
        />
      </LocaleProvider>
    );
  }

  return <>{children}</>;
}
