import { useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useIntl } from "react-intl";
import type { ConfigGeneralInput } from "@/core/generated-graphql";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export function CredentialsDialog({
  onOpenChange,
  initialUsername,
  passwordConfigured,
  onSave,
}: {
  onOpenChange: (open: boolean) => void;
  initialUsername: string;
  passwordConfigured: boolean;
  onSave: (input: ConfigGeneralInput) => Promise<void>;
}) {
  const intl = useIntl();
  const [failed, setFailed] = useState(false);
  const form = useForm({
    defaultValues: {
      username: initialUsername,
      newPassword: "",
      clearPassword: false,
    },
    validators: {
      onChange: z.object({
        username: z.string(),
        newPassword: z.string().refine(
          (value) => new TextEncoder().encode(value).length <= 72,
          intl.formatMessage({
            id: "config.general.auth.password_length",
            defaultMessage: "Use at most 72 UTF-8 bytes for the password.",
          }),
        ),
        clearPassword: z.boolean(),
      }),
    },
    onSubmit: async ({ value }) => {
      setFailed(false);
      try {
        await onSave({
          username: value.username,
          ...(value.clearPassword
            ? { clearPassword: true }
            : value.newPassword
              ? { newPassword: value.newPassword }
              : {}),
        });
        onOpenChange(false);
      } catch {
        setFailed(true);
      }
    },
  });

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false}>
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
              defaultMessage: "Credentials to restrict access to Stash.",
            })}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Subscribe
            selector={(state) =>
              [state.isSubmitting, state.values.clearPassword] as const
            }
          >
            {([saving, clearing]) => (
              <FieldGroup>
                <form.Field name="username">
                  {(field) => (
                    <Field>
                      <FieldLabel htmlFor="auth-username">
                        {intl.formatMessage({
                          id: "config.general.auth.username",
                          defaultMessage: "Username",
                        })}
                      </FieldLabel>
                      <Input
                        id="auth-username"
                        name="username"
                        autoComplete="username"
                        disabled={saving}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(event) =>
                          field.handleChange(event.currentTarget.value)
                        }
                      />
                    </Field>
                  )}
                </form.Field>
                <form.Field name="newPassword">
                  {(field) => (
                    <Field data-invalid={!field.state.meta.isValid}>
                      <FieldLabel htmlFor="auth-password">
                        {intl.formatMessage({
                          id: "config.general.auth.password",
                          defaultMessage: "Password",
                        })}
                      </FieldLabel>
                      <Input
                        id="auth-password"
                        name="password"
                        type="password"
                        autoComplete="new-password"
                        disabled={saving || clearing}
                        value={field.state.value}
                        onBlur={field.handleBlur}
                        onChange={(event) =>
                          field.handleChange(event.currentTarget.value)
                        }
                        aria-invalid={!field.state.meta.isValid}
                        aria-describedby="auth-password-description"
                      />
                      <FieldDescription id="auth-password-description">
                        {intl.formatMessage({
                          id: "config.general.auth.password_preserve",
                          defaultMessage:
                            "Leave blank to keep the current password.",
                        })}
                      </FieldDescription>
                      <FieldError errors={field.state.meta.errors} />
                    </Field>
                  )}
                </form.Field>
                {passwordConfigured && (
                  <form.Field name="clearPassword">
                    {(field) => (
                      <Field orientation="horizontal">
                        <Checkbox
                          id="clear-password"
                          checked={field.state.value}
                          disabled={saving}
                          onCheckedChange={(checked) => {
                            field.handleChange(checked);
                            if (checked) form.setFieldValue("newPassword", "");
                          }}
                        />
                        <FieldLabel htmlFor="clear-password">
                          {intl.formatMessage({
                            id: "config.general.auth.clear_password",
                            defaultMessage:
                              "Remove the password and disable password authentication",
                          })}
                        </FieldLabel>
                      </Field>
                    )}
                  </form.Field>
                )}
                {failed && (
                  <Alert variant="destructive">
                    <AlertDescription>
                      {intl.formatMessage({
                        id: "config.general.auth.save_failed",
                        defaultMessage:
                          "Could not confirm that credentials were saved. Your changes are still here; try again.",
                      })}
                    </AlertDescription>
                  </Alert>
                )}
                <DialogFooter>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={saving}
                    onClick={() => onOpenChange(false)}
                  >
                    {intl.formatMessage({
                      id: "actions.cancel",
                      defaultMessage: "Cancel",
                    })}
                  </Button>
                  <Button type="submit" disabled={saving}>
                    {saving && <Spinner data-icon="inline-start" />}
                    {intl.formatMessage({
                      id: "actions.save",
                      defaultMessage: "Save",
                    })}
                  </Button>
                </DialogFooter>
              </FieldGroup>
            )}
          </form.Subscribe>
        </form>
      </DialogContent>
    </Dialog>
  );
}
