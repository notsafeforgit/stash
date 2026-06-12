import { useState } from "react";
import { createFileRoute } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { useConfigurationContext, useConfigureGeneral } from "src/hooks/config";
import { useToast } from "src/hooks/toast";
import { useMsg } from "src/hooks/message";
import { Button } from "src/components/ui/button";
import { Input } from "src/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "src/components/ui/field";
import {
  SettingDisplay,
  SettingNumber,
  SettingsSection,
} from "src/components/settings/setting-row";

function CredentialsDialog({
  open,
  onOpenChange,
  initialUsername,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialUsername: string;
  onSave: (username: string, password: string) => void;
}) {
  const intl = useIntl();
  const [username, setUsername] = useState(initialUsername);
  const [password, setPassword] = useState("");

  // Re-seed the draft each time the dialog opens.
  const [syncedOpen, setSyncedOpen] = useState(open);
  if (syncedOpen !== open) {
    setSyncedOpen(open);
    if (open) {
      setUsername(initialUsername);
      setPassword("");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "config.general.auth.credentials.heading",
              defaultMessage: "Credentials",
            })}
          </DialogTitle>
          <DialogDescription>
            {intl.formatMessage({
              id: "config.general.auth.credentials.description",
              defaultMessage:
                "Username and password to access stash. Leave blank to disable authentication.",
            })}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Field>
            <FieldLabel htmlFor="auth-username">
              {intl.formatMessage({
                id: "config.general.auth.username",
                defaultMessage: "Username",
              })}
            </FieldLabel>
            <Input
              id="auth-username"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.currentTarget.value)}
            />
            <FieldDescription>
              {intl.formatMessage({
                id: "config.general.auth.username_desc",
                defaultMessage:
                  "Username to access stash. Leave blank to disable user authentication",
              })}
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="auth-password">
              {intl.formatMessage({
                id: "config.general.auth.password",
                defaultMessage: "Password",
              })}
            </FieldLabel>
            <Input
              id="auth-password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.currentTarget.value)}
            />
            <FieldDescription>
              {intl.formatMessage({
                id: "config.general.auth.password_desc",
                defaultMessage:
                  "Password to access stash. Leave blank to disable user authentication",
              })}
            </FieldDescription>
          </Field>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            type="button"
            onClick={() => {
              onSave(username, password);
              onOpenChange(false);
            }}
          >
            {intl.formatMessage({ id: "actions.save", defaultMessage: "Save" })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SettingsSecurityPage() {
  const Toast = useToast();
  const { configuration } = useConfigurationContext();
  const general = configuration.general;
  const [configureGeneral] = useConfigureGeneral();
  const [generateAPIKey] = useMutation(GQL.GenerateApiKeyDocument, {
    refetchQueries: [{ query: GQL.ConfigurationDocument }],
  });

  const [credentialsOpen, setCredentialsOpen] = useState(false);

  const msg = useMsg();

  async function onGenerateAPIKey(clear: boolean) {
    try {
      await generateAPIKey({
        variables: { input: clear ? { clear: true } : {} },
      });
    } catch (e) {
      Toast.error(e);
    }
  }

  return (
    <div className="max-w-3xl space-y-8 p-6">
      <SettingsSection
        title={msg("config.general.auth.authentication", "Authentication")}
      >
        <SettingDisplay
          label={msg("config.general.auth.credentials.heading", "Credentials")}
          description={msg(
            "config.general.auth.credentials.description",
            "Username and password to access stash.",
          )}
          value={general.username || undefined}
          actions={
            <Button
              type="button"
              variant="outline"
              onClick={() => setCredentialsOpen(true)}
            >
              {msg("actions.edit", "Edit")}…
            </Button>
          }
        />
        <SettingDisplay
          label={msg("config.general.auth.api_key", "API key")}
          description={msg(
            "config.general.auth.api_key_desc",
            "API key for authenticating external clients. Requires username and password to be set.",
          )}
          value={general.apiKey || undefined}
          actions={
            <>
              <Button
                type="button"
                variant="outline"
                disabled={!general.username || !general.password}
                onClick={() => void onGenerateAPIKey(false)}
              >
                {msg(
                  "config.general.auth.generate_api_key",
                  "Generate API key",
                )}
              </Button>
              <Button
                type="button"
                variant="destructive"
                disabled={!general.apiKey}
                onClick={() => void onGenerateAPIKey(true)}
              >
                {msg("config.general.auth.clear_api_key", "Clear API key")}
              </Button>
            </>
          }
        />
        <SettingNumber
          label={msg(
            "config.general.auth.maximum_session_age",
            "Maximum session age",
          )}
          description={msg(
            "config.general.auth.maximum_session_age_desc",
            "Maximum idle time before a login session is expired, in seconds.",
          )}
          value={general.maxSessionAge}
          onChange={(v) =>
            void configureGeneral({
              variables: { input: { maxSessionAge: v } },
            })
          }
          min={0}
          integer
        />
      </SettingsSection>

      <CredentialsDialog
        open={credentialsOpen}
        onOpenChange={setCredentialsOpen}
        initialUsername={general.username}
        onSave={(username, password) =>
          void configureGeneral({
            variables: { input: { username, password } },
          })
        }
      />
    </div>
  );
}

export const Route = createFileRoute("/settings/security")({
  component: SettingsSecurityPage,
});
