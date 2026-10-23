import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@apollo/client/react";
import * as GQL from "@/core/generated-graphql";
import { Alert, AlertDescription } from "@/components/ui/alert";
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
  Progress,
  ProgressLabel,
  ProgressValue,
  ProgressTrack,
  ProgressIndicator,
} from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { useMsg } from "@/hooks/message";
import { useToast } from "@/hooks/toast";
import { ShareLinkDialog } from "./share-link-dialog";

export type ShareResult = { id: string; url?: string | null };

export function ShareResultDialog({
  link,
  onClose,
}: {
  link: ShareResult;
  onClose: () => void;
}) {
  if (link.url)
    return (
      <ShareLinkDialog
        link={{ id: link.id, url: link.url }}
        onClose={onClose}
      />
    );
  return <SharePreparationDialog id={link.id} onClose={onClose} />;
}

function SharePreparationDialog({
  id,
  onClose,
}: {
  id: string;
  onClose: () => void;
}) {
  const msg = useMsg();
  const toast = useToast();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 2000);
    return () => clearInterval(timer);
  }, []);
  const { data, error, loading, refetch, startPolling, stopPolling } = useQuery(
    GQL.MediaShareDocument,
    {
      variables: { id },
      fetchPolicy: "cache-and-network",
      pollInterval: 2000,
      skipPollAttempt: () => document.hidden,
    },
  );
  const [retry, { loading: retrying }] = useMutation(
    GQL.MediaShareRetryDocument,
  );
  const share = data?.mediaShare;
  const active =
    share && !share.revoked_at && new Date(share.expires_at).getTime() > now;
  const shouldPoll = !share?.url && (!share || active);
  useEffect(() => {
    if (shouldPoll) startPolling(2000);
    else stopPolling();
    return stopPolling;
  }, [shouldPoll, startPolling, stopPolling]);
  if (share?.url)
    return <ShareLinkDialog link={{ id, url: share.url }} onClose={onClose} />;
  const failed = share?.preparation_state === GQL.SharePreparationState.Failed;
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {msg("sharing.preparing", "Preparing share")}
          </DialogTitle>
          <DialogDescription>
            {msg(
              "sharing.preparation_description",
              "Your selection is saved. The link will be available when preparation finishes. You can close this dialog and return from Settings → Shares.",
            )}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>
              {msg(
                "sharing.preparation_connection",
                "Unable to check preparation. Your saved selection is retained.",
              )}
            </AlertDescription>
            <Button variant="outline" onClick={() => void refetch()}>
              {msg("actions.retry", "Retry")}
            </Button>
          </Alert>
        ) : !share && loading ? (
          <Spinner />
        ) : !active ? (
          <Alert>
            <AlertDescription>
              {msg(
                "sharing.unavailable_description",
                "This link has expired, was revoked, or is invalid.",
              )}
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <Progress value={share.prepared_count} max={share.media_count || 1}>
              <ProgressLabel>
                {msg("sharing.prepared", "Media prepared")}
              </ProgressLabel>
              <ProgressValue>
                {() =>
                  `${share.prepared_count.toLocaleString()} / ${(share.media_count ?? 0).toLocaleString()}`
                }
              </ProgressValue>
              <ProgressTrack>
                <ProgressIndicator />
              </ProgressTrack>
            </Progress>
            {failed && (
              <Alert variant="destructive">
                <AlertDescription>{share.preparation_error}</AlertDescription>
              </Alert>
            )}
          </>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {msg("actions.close", "Close")}
          </Button>
          {active && failed && (
            <Button
              disabled={retrying}
              onClick={async () => {
                try {
                  await retry({ variables: { id } });
                  await refetch();
                } catch (cause) {
                  toast.error(cause);
                }
              }}
            >
              {retrying && <Spinner data-icon="inline-start" />}
              {msg("sharing.retry_preparation", "Retry preparation")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
