import { useSyncExternalStore } from "react";
import { useIntl } from "react-intl";
import { sessionRecovery } from "@/core/session-recovery";
import { LoginForm, LoginLocale } from "./login-form";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

function RecoveryDialog({ open }: { open: boolean }) {
  const intl = useIntl();
  return (
    <Dialog open={open}>
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "login.session_expired",
              defaultMessage: "Sign in to continue",
            })}
          </DialogTitle>
          <DialogDescription>
            {intl.formatMessage({
              id: "login.edits_retained",
              defaultMessage: "Your edits are kept while you sign in.",
            })}
          </DialogDescription>
        </DialogHeader>
        <LoginForm onSuccess={sessionRecovery.authenticated} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted beside App so initial configuration requests can also recover. */
export function SessionRecoveryDialog() {
  const open = useSyncExternalStore(
    sessionRecovery.subscribe,
    sessionRecovery.getSnapshot,
  );
  if (!open) return null;
  return (
    <LoginLocale>
      <RecoveryDialog open />
    </LoginLocale>
  );
}
