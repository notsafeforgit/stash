import { useEffect, useState, type ReactNode } from "react";
import { useForm } from "@tanstack/react-form";
import { IntlProvider, useIntl } from "react-intl";
import { z } from "zod";
import { getPlatformURL } from "@/core/platform-url";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";

const localeSchema = z.object({
  locale: z.string(),
  messages: z.record(z.string(), z.string()),
});

/** Login translations are public and do not require owner configuration. */
export function LoginLocale({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<z.infer<typeof localeSchema>>({
    locale: "en-GB",
    messages: {},
  });
  useEffect(() => {
    const controller = new AbortController();
    void fetch(getPlatformURL("login/locale"), {
      signal: controller.signal,
      credentials: "same-origin",
    })
      .then(async (response) =>
        response.ok ? localeSchema.parse(await response.json()) : null,
      )
      .then((result) => {
        if (result) setLocale(result);
      })
      .catch(() => {
        /* Default messages remain usable offline or during restart. */
      });
    return () => controller.abort();
  }, []);
  return (
    <IntlProvider
      locale={locale.locale}
      defaultLocale="en-GB"
      messages={locale.messages}
    >
      {children}
    </IntlProvider>
  );
}

/** Shared by the standalone page and in-place session recovery. */
export function LoginForm({ onSuccess }: { onSuccess: () => void }) {
  const intl = useIntl();
  const [failure, setFailure] = useState<
    "invalid_credentials" | "internal_error" | null
  >(null);
  const form = useForm({
    defaultValues: { username: "", password: "" },
    validators: {
      onChange: z.object({
        username: z.string().min(
          1,
          intl.formatMessage({
            id: "login.username_required",
            defaultMessage: "Enter your username.",
          }),
        ),
        password: z.string().min(
          1,
          intl.formatMessage({
            id: "login.password_required",
            defaultMessage: "Enter your password.",
          }),
        ),
      }),
    },
    onSubmit: async ({ value }) => {
      setFailure(null);
      try {
        const response = await fetch(getPlatformURL("login"), {
          method: "POST",
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          body: new URLSearchParams(value),
        });
        if (response.ok) onSuccess();
        else
          setFailure(
            response.status === 401 ? "invalid_credentials" : "internal_error",
          );
      } catch {
        setFailure("internal_error");
      }
    },
  });
  return (
    <form
      action={getPlatformURL("login").href}
      method="post"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <form.Subscribe selector={(state) => state.isSubmitting}>
        {(pending) => (
          <FieldGroup>
            <form.Field name="username">
              {(field) => (
                <Field
                  data-invalid={
                    field.state.meta.isTouched && !field.state.meta.isValid
                  }
                >
                  <FieldLabel htmlFor="login-username">
                    {intl.formatMessage({
                      id: "login.username",
                      defaultMessage: "Username",
                    })}
                  </FieldLabel>
                  <Input
                    id="login-username"
                    name="username"
                    autoComplete="username"
                    autoCapitalize="none"
                    spellCheck={false}
                    required
                    disabled={pending}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) =>
                      field.handleChange(event.currentTarget.value)
                    }
                    aria-invalid={
                      field.state.meta.isTouched && !field.state.meta.isValid
                    }
                  />
                  {field.state.meta.isTouched && (
                    <FieldError errors={field.state.meta.errors} />
                  )}
                </Field>
              )}
            </form.Field>
            <form.Field name="password">
              {(field) => (
                <Field
                  data-invalid={
                    field.state.meta.isTouched && !field.state.meta.isValid
                  }
                >
                  <FieldLabel htmlFor="login-password">
                    {intl.formatMessage({
                      id: "login.password",
                      defaultMessage: "Password",
                    })}
                  </FieldLabel>
                  <Input
                    id="login-password"
                    name="password"
                    type="password"
                    autoComplete="current-password"
                    required
                    disabled={pending}
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(event) =>
                      field.handleChange(event.currentTarget.value)
                    }
                    aria-invalid={
                      field.state.meta.isTouched && !field.state.meta.isValid
                    }
                  />
                  {field.state.meta.isTouched && (
                    <FieldError errors={field.state.meta.errors} />
                  )}
                </Field>
              )}
            </form.Field>
            {failure && (
              <Alert variant="destructive">
                <AlertDescription>
                  {failure === "invalid_credentials"
                    ? intl.formatMessage({
                        id: "login.invalid_credentials",
                        defaultMessage: "Invalid username or password",
                      })
                    : intl.formatMessage({
                        id: "login.internal_error",
                        defaultMessage:
                          "Unexpected internal error. See logs for more details",
                      })}
                </AlertDescription>
              </Alert>
            )}
            <Button type="submit" disabled={pending}>
              {pending && <Spinner data-icon="inline-start" />}
              {intl.formatMessage({
                id: "login.login",
                defaultMessage: "Login",
              })}
            </Button>
          </FieldGroup>
        )}
      </form.Subscribe>
    </form>
  );
}
