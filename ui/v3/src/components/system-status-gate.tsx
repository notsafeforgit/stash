import { useQuery } from "@apollo/client/react";
import { CatchBoundary } from "@tanstack/react-router";
import { Suspense, type PropsWithChildren } from "react";
import { DEFAULT_LOCALE, LocaleProvider } from "@/components/locale-provider";
import * as GQL from "@/core/generated-graphql";
import { lazyComponent } from "@/utils/lazy-component";
import { StartupError } from "./query-error";
import { FullPageSpinner } from "./full-page-spinner";

const MigrationRequiredDialog = lazyComponent(async () => ({
  default: (await import("./migration-gate")).MigrationRequiredDialog,
}));
const SetupWizard = lazyComponent(async () => ({
  default: (await import("./setup-wizard")).SetupWizard,
}));

export function SystemStatusGate({ children }: PropsWithChildren) {
  const { data, loading, error, refetch } = useQuery(GQL.SystemStatusDocument, {
    fetchPolicy: "network-only",
  });

  if (!data && error) {
    return <StartupError error={error} retry={refetch} retrying={loading} />;
  }

  if (!data) {
    return <FullPageSpinner />;
  }

  if (data?.systemStatus.status === GQL.SystemStatusEnum.NeedsMigration) {
    return (
      <CatchBoundary
        getResetKey={() => data.systemStatus.status}
        errorComponent={StartupChunkError}
      >
        <LocaleProvider language={DEFAULT_LOCALE}>
          <Suspense fallback={<FullPageSpinner />}>
            <MigrationRequiredDialog
              status={data.systemStatus}
              onComplete={async () => {
                await refetch();
              }}
            />
          </Suspense>
        </LocaleProvider>
      </CatchBoundary>
    );
  }

  if (data?.systemStatus.status === GQL.SystemStatusEnum.Setup) {
    return (
      <CatchBoundary
        getResetKey={() => data.systemStatus.status}
        errorComponent={StartupChunkError}
      >
        <LocaleProvider language={DEFAULT_LOCALE}>
          <Suspense fallback={<FullPageSpinner />}>
            <SetupWizard
              status={data.systemStatus}
              onComplete={async () => {
                await refetch();
              }}
            />
          </Suspense>
        </LocaleProvider>
      </CatchBoundary>
    );
  }

  return <>{children}</>;
}

function StartupChunkError({ error }: { error: unknown }) {
  return (
    <StartupError
      error={error instanceof Error ? error : new Error(String(error))}
      retry={async () => window.location.reload()}
    />
  );
}
