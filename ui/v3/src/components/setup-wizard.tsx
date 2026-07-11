import { useApolloClient, useMutation, useQuery } from "@apollo/client/react";
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  CheckIcon,
  EyeIcon,
  EyeOffIcon,
  FolderIcon,
  MinusIcon,
  PlusIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { FormattedMessage, useIntl } from "react-intl";
import { FolderSelect } from "@/components/shared/folder-select";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { getPlatformURL } from "@/core/create-client";
import * as GQL from "@/core/generated-graphql";
import { stripQuotes } from "@/utils/text";

type SystemStatus = GQL.SystemStatusQuery["systemStatus"];
type SetupStep = "configuration" | "storage" | "access" | "review";
type SetupOverrides = Pick<
  GQL.ConfigGeneralDataFragment,
  "databasePath" | "generatedPath" | "cachePath" | "blobsPath"
>;

const STEPS: SetupStep[] = ["configuration", "storage", "access", "review"];

function defaultSetupInput(status: SystemStatus): GQL.SetupInput {
  return {
    configLocation: status.configPath ?? "",
    stashes: [],
    sfwContentMode: false,
    databaseFile: "",
    generatedLocation: "",
    cacheLocation: "",
    storeBlobsInDatabase: false,
    blobsLocation: "",
  };
}

function resolvedSetupPaths(
  status: SystemStatus,
  input: GQL.SetupInput,
  overrides?: SetupOverrides,
) {
  const separator = status.os === "windows" ? "\\" : "/";
  const home =
    status.homeDir || (status.os === "windows" ? "%USERPROFILE%" : "$HOME");
  const configLocation =
    input.configLocation || [home, ".stash", "config.yml"].join(separator);
  const lastSeparator = Math.max(
    configLocation.lastIndexOf("/"),
    configLocation.lastIndexOf("\\"),
  );
  const configDirectory =
    lastSeparator >= 0 ? configLocation.slice(0, lastSeparator) : "";
  const relativeToConfig = (name: string) =>
    configDirectory ? `${configDirectory}${separator}${name}` : name;

  return {
    configLocation,
    databaseFile:
      overrides?.databasePath ||
      input.databaseFile ||
      relativeToConfig("stash-go.sqlite"),
    generatedLocation:
      overrides?.generatedPath ||
      input.generatedLocation ||
      relativeToConfig("generated"),
    cacheLocation:
      overrides?.cachePath || input.cacheLocation || relativeToConfig("cache"),
    blobsLocation:
      overrides?.blobsPath || input.blobsLocation || relativeToConfig("blobs"),
  };
}

function SetupNavigation({
  step,
  busy,
  nextDisabled,
  onBack,
  onNext,
}: {
  step: number;
  busy: boolean;
  nextDisabled?: boolean;
  onBack: () => void;
  onNext: () => void;
}) {
  const isLast = step === STEPS.length - 1;

  return (
    <div className="flex items-center justify-between gap-3 border-t px-5 py-4 sm:px-8">
      <Button
        type="button"
        variant="outline"
        onClick={onBack}
        disabled={step === 0 || busy}
      >
        <ArrowLeftIcon data-icon="inline-start" />
        <FormattedMessage id="actions.previous_action" />
      </Button>
      <Button type="button" onClick={onNext} disabled={busy || nextDisabled}>
        {busy ? (
          <Spinner data-icon="inline-start" />
        ) : isLast ? (
          <CheckIcon data-icon="inline-start" />
        ) : null}
        <FormattedMessage
          id={isLast ? "actions.confirm" : "actions.next_action"}
        />
        {!busy && !isLast && <ArrowRightIcon data-icon="inline-end" />}
      </Button>
    </div>
  );
}

function ConfigurationStep({
  status,
  value,
  onChange,
}: {
  status: SystemStatus;
  value: string;
  onChange: (value: string) => void;
}) {
  const windows = status.os === "windows";
  const separator = windows ? "\\" : "/";
  const home = status.homeDir || (windows ? "%USERPROFILE%" : "$HOME");
  const workingDirectory = status.workingDir || ".";
  const defaultPath = [home, ".stash", "config.yml"].join(separator);
  const isMacApp = status.os === "darwin" && workingDirectory === "/";

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h2 className="text-xl font-semibold">
          <FormattedMessage id="setup.welcome_to_stash" />
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          <FormattedMessage
            id={
              status.configPath
                ? "setup.welcome_specific_config.unable_to_locate_specified_config"
                : "setup.welcome.unable_to_locate_config"
            }
          />
        </p>
      </header>

      {status.configPath ? (
        <div className="rounded-lg border bg-muted/30 p-4">
          <p className="text-sm text-muted-foreground">
            <FormattedMessage
              id="setup.welcome_specific_config.config_path"
              values={{
                path: status.configPath,
                code: (chunks) => <code>{chunks}</code>,
              }}
            />
          </p>
        </div>
      ) : (
        <Field>
          <FieldLabel>
            <FormattedMessage id="setup.welcome.store_stash_config" />
          </FieldLabel>
          <RadioGroup
            value={value === "config.yml" ? "working" : "home"}
            onValueChange={(location) =>
              onChange(location === "working" ? "config.yml" : "")
            }
          >
            <Label className="items-start rounded-lg border p-4 leading-normal has-data-checked:border-primary has-data-checked:bg-primary/5">
              <RadioGroupItem value="home" className="mt-0.5" />
              <span className="min-w-0 space-y-1">
                <span className="block font-medium">
                  <FormattedMessage
                    id="setup.configuration.user_directory"
                    defaultMessage="Stash user directory"
                  />
                </span>
                <code className="block break-all text-xs font-normal text-muted-foreground">
                  {defaultPath}
                </code>
              </span>
            </Label>
            <Label
              className="items-start rounded-lg border p-4 leading-normal has-data-checked:border-primary has-data-checked:bg-primary/5 data-disabled:cursor-not-allowed data-disabled:opacity-50"
              data-disabled={isMacApp || undefined}
            >
              <RadioGroupItem
                value="working"
                className="mt-0.5"
                disabled={isMacApp}
              />
              <span className="min-w-0 space-y-1">
                <span className="block font-medium">
                  <FormattedMessage
                    id="setup.configuration.working_directory"
                    defaultMessage="Current working directory"
                  />
                </span>
                <code className="block break-all text-xs font-normal text-muted-foreground">
                  {workingDirectory}
                  {separator}config.yml
                </code>
                {isMacApp && (
                  <span className="block text-xs font-normal text-muted-foreground">
                    <FormattedMessage id="setup.welcome.in_the_current_working_directory_disabled_macos" />
                  </span>
                )}
              </span>
            </Label>
          </RadioGroup>
          <FieldDescription>
            <FormattedMessage
              id="setup.welcome.unexpected_explained"
              values={{ code: (chunks) => <code>{chunks}</code> }}
            />
          </FieldDescription>
        </Field>
      )}
    </div>
  );
}

function LibraryEditor({
  stashes,
  initialDirectory,
  onChange,
}: {
  stashes: GQL.StashConfigInput[];
  initialDirectory: string;
  onChange: (stashes: GQL.StashConfigInput[]) => void;
}) {
  const intl = useIntl();
  const [directory, setDirectory] = useState(initialDirectory);

  function update(index: number, patch: Partial<GQL.StashConfigInput>) {
    onChange(
      stashes.map((stash, current) =>
        current === index ? { ...stash, ...patch } : stash,
      ),
    );
  }

  function addDirectory() {
    const path = stripQuotes(directory.trim());
    if (!path || stashes.some((stash) => stash.path === path)) return;
    onChange([...stashes, { path, excludeVideo: false, excludeImage: false }]);
    setDirectory(initialDirectory);
  }

  return (
    <div className="space-y-3">
      {stashes.map((stash, index) => (
        <div
          key={stash.path}
          className="flex flex-col gap-3 rounded-lg border p-3 sm:flex-row sm:items-center"
        >
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <FolderIcon className="size-4 shrink-0 text-muted-foreground" />
            <code className="truncate text-sm" title={stash.path}>
              {stash.path}
            </code>
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <Label className="font-normal">
              <Checkbox
                checked={stash.excludeVideo}
                onCheckedChange={(checked) =>
                  update(index, { excludeVideo: checked === true })
                }
              />
              <FormattedMessage id="config.general.exclude_video" />
            </Label>
            <Label className="font-normal">
              <Checkbox
                checked={stash.excludeImage}
                onCheckedChange={(checked) =>
                  update(index, { excludeImage: checked === true })
                }
              />
              <FormattedMessage id="config.general.exclude_image" />
            </Label>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() =>
                onChange(stashes.filter((_, current) => current !== index))
              }
              aria-label={intl.formatMessage({ id: "actions.delete" })}
            >
              <MinusIcon />
            </Button>
          </div>
        </div>
      ))}
      <FolderSelect
        currentDirectory={directory}
        onChangeDirectory={setDirectory}
        appendButton={
          <Button
            type="button"
            variant="secondary"
            size="icon"
            onClick={addDirectory}
            disabled={!directory.trim()}
            aria-label={intl.formatMessage({ id: "actions.add_directory" })}
          >
            <PlusIcon />
          </Button>
        }
      />
    </div>
  );
}

function StorageStep({
  status,
  input,
  overrides,
  onChange,
  emptyLibraryWarning,
}: {
  status: SystemStatus;
  input: GQL.SetupInput;
  overrides?: SetupOverrides;
  onChange: (patch: Partial<GQL.SetupInput>) => void;
  emptyLibraryWarning: boolean;
}) {
  return (
    <div className="space-y-8">
      <header className="space-y-2">
        <h2 className="text-xl font-semibold">
          <FormattedMessage id="setup.paths.set_up_your_paths" />
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          <FormattedMessage id="setup.paths.description" />
        </p>
      </header>

      <section className="space-y-3">
        <div>
          <h3 className="text-sm font-medium">
            <FormattedMessage id="setup.paths.where_is_your_porn_located" />
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            <FormattedMessage id="setup.paths.where_is_your_porn_located_description" />
          </p>
        </div>
        <LibraryEditor
          stashes={input.stashes}
          initialDirectory={status.homeDir || status.workingDir || ""}
          onChange={(stashes) => onChange({ stashes })}
        />
        {emptyLibraryWarning && (
          <div className="flex gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
            <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
            <FormattedMessage id="setup.paths.stash_alert" />
          </div>
        )}
      </section>

      <section className="space-y-4">
        <h3 className="text-sm font-medium">
          <FormattedMessage
            id="setup.storage.locations"
            defaultMessage="Storage locations"
          />
        </h3>
        <FieldGroup className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="setup-database">
              <FormattedMessage id="setup.confirm.database_file_path" />
            </FieldLabel>
            <Input
              id="setup-database"
              value={overrides?.databasePath || input.databaseFile}
              onChange={(event) =>
                onChange({ databaseFile: event.currentTarget.value })
              }
              placeholder="stash-go.sqlite"
              spellCheck={false}
              disabled={Boolean(overrides?.databasePath)}
            />
            <FieldDescription>
              <FormattedMessage id="setup.paths.database_filename_empty_for_default" />
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="setup-generated">
              <FormattedMessage id="setup.confirm.generated_directory" />
            </FieldLabel>
            <Input
              id="setup-generated"
              value={overrides?.generatedPath || input.generatedLocation}
              onChange={(event) =>
                onChange({ generatedLocation: event.currentTarget.value })
              }
              placeholder="generated"
              spellCheck={false}
              disabled={Boolean(overrides?.generatedPath)}
            />
            <FieldDescription>
              <FormattedMessage id="setup.paths.path_to_generated_directory_empty_for_default" />
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="setup-cache">
              <FormattedMessage id="setup.confirm.cache_directory" />
            </FieldLabel>
            <Input
              id="setup-cache"
              value={overrides?.cachePath || input.cacheLocation}
              onChange={(event) =>
                onChange({ cacheLocation: event.currentTarget.value })
              }
              placeholder="cache"
              spellCheck={false}
              disabled={Boolean(overrides?.cachePath)}
            />
            <FieldDescription>
              <FormattedMessage id="setup.paths.path_to_cache_directory_empty_for_default" />
            </FieldDescription>
          </Field>
          <Field>
            <FieldLabel htmlFor="setup-blobs">
              <FormattedMessage id="setup.confirm.blobs_directory" />
            </FieldLabel>
            <Input
              id="setup-blobs"
              value={overrides?.blobsPath || input.blobsLocation}
              onChange={(event) =>
                onChange({ blobsLocation: event.currentTarget.value })
              }
              placeholder="blobs"
              spellCheck={false}
              disabled={
                input.storeBlobsInDatabase || Boolean(overrides?.blobsPath)
              }
            />
            <FieldDescription>
              <FormattedMessage id="setup.paths.path_to_blobs_directory_empty_for_default" />
            </FieldDescription>
          </Field>
        </FieldGroup>
      </section>

      <section className="divide-y rounded-lg border">
        <Label className="cursor-pointer justify-between gap-4 p-4 leading-normal">
          <span>
            <span className="block font-medium">
              <FormattedMessage id="setup.paths.store_blobs_in_database" />
            </span>
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              <FormattedMessage id="setup.storage.database_blobs_description" />
            </span>
          </span>
          <Switch
            checked={input.storeBlobsInDatabase}
            onCheckedChange={(checked) =>
              onChange({
                storeBlobsInDatabase: checked,
                blobsLocation: checked ? "" : input.blobsLocation,
              })
            }
          />
        </Label>
        <Label className="cursor-pointer justify-between gap-4 p-4 leading-normal">
          <span>
            <span className="block font-medium">
              <FormattedMessage id="setup.paths.use_sfw_content_mode" />
            </span>
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              <FormattedMessage id="setup.paths.sfw_content_settings_description" />
            </span>
          </span>
          <Switch
            checked={input.sfwContentMode ?? false}
            onCheckedChange={(checked) => onChange({ sfwContentMode: checked })}
          />
        </Label>
      </section>
    </div>
  );
}

function AccessStep({
  input,
  onChange,
}: {
  input: GQL.SetupInput;
  onChange: (patch: Partial<GQL.SetupInput>) => void;
}) {
  const intl = useIntl();
  const [showPassword, setShowPassword] = useState(false);
  const username = input.initialUsername ?? "";
  const password = input.initialPassword ?? "";
  const usernameInvalid = username.trim() !== username;
  const passwordInvalid = Boolean(username) && !password;

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h2 className="text-xl font-semibold">
          <FormattedMessage id="setup.credentials.heading" />
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          <FormattedMessage id="setup.credentials.description" />
        </p>
      </header>

      <FieldGroup>
        <Field data-invalid={usernameInvalid || undefined}>
          <FieldLabel htmlFor="setup-username">
            <FormattedMessage id="login.username" />
          </FieldLabel>
          <Input
            id="setup-username"
            value={username}
            aria-invalid={usernameInvalid || undefined}
            autoComplete="username"
            onChange={(event) =>
              onChange({ initialUsername: event.currentTarget.value })
            }
          />
          {usernameInvalid && (
            <FieldDescription>
              <FormattedMessage id="setup.credentials.username_invalid" />
            </FieldDescription>
          )}
        </Field>
        <Field data-invalid={passwordInvalid || undefined}>
          <FieldLabel htmlFor="setup-password">
            <FormattedMessage id="login.password" />
          </FieldLabel>
          <div className="flex gap-2">
            <Input
              id="setup-password"
              type={showPassword ? "text" : "password"}
              value={password}
              aria-invalid={passwordInvalid || undefined}
              autoComplete="new-password"
              onChange={(event) =>
                onChange({ initialPassword: event.currentTarget.value })
              }
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => setShowPassword((value) => !value)}
              aria-label={intl.formatMessage({
                id: showPassword ? "actions.hide" : "actions.show",
              })}
            >
              {showPassword ? <EyeOffIcon /> : <EyeIcon />}
            </Button>
          </div>
          {passwordInvalid && (
            <FieldDescription>
              <FormattedMessage id="setup.credentials.password_invalid" />
            </FieldDescription>
          )}
        </Field>
      </FieldGroup>
    </div>
  );
}

function ReviewRow({
  label,
  children,
}: {
  label: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1 border-b py-3 last:border-b-0 sm:grid-cols-[12rem_1fr] sm:gap-4">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words text-sm">{children}</dd>
    </div>
  );
}

function ReviewStep({
  status,
  input,
  overrides,
}: {
  status: SystemStatus;
  input: GQL.SetupInput;
  overrides?: SetupOverrides;
}) {
  const credentialsEnabled = Boolean(input.initialUsername);
  const paths = resolvedSetupPaths(status, input, overrides);

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h2 className="text-xl font-semibold">
          <FormattedMessage id="setup.confirm.nearly_there" />
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          <FormattedMessage id="setup.confirm.almost_ready" />
        </p>
      </header>

      <dl>
        <ReviewRow
          label={
            <FormattedMessage id="setup.confirm.configuration_file_location" />
          }
        >
          <code>{paths.configLocation}</code>
        </ReviewRow>
        <ReviewRow
          label={
            <FormattedMessage id="setup.confirm.stash_library_directories" />
          }
        >
          {input.stashes.length > 0 ? (
            <ul className="space-y-1">
              {input.stashes.map((stash) => (
                <li key={stash.path}>
                  <code>{stash.path}</code>
                </li>
              ))}
            </ul>
          ) : (
            <FormattedMessage id="setup.folder.no_paths_added" />
          )}
        </ReviewRow>
        <ReviewRow
          label={<FormattedMessage id="setup.confirm.database_file_path" />}
        >
          <code>{paths.databaseFile}</code>
        </ReviewRow>
        <ReviewRow
          label={<FormattedMessage id="setup.confirm.generated_directory" />}
        >
          <code>{paths.generatedLocation}</code>
        </ReviewRow>
        <ReviewRow
          label={<FormattedMessage id="setup.confirm.cache_directory" />}
        >
          <code>{paths.cacheLocation}</code>
        </ReviewRow>
        <ReviewRow
          label={<FormattedMessage id="setup.confirm.blobs_directory" />}
        >
          <code>
            {input.storeBlobsInDatabase ? (
              <FormattedMessage id="setup.confirm.blobs_use_database" />
            ) : (
              paths.blobsLocation
            )}
          </code>
        </ReviewRow>
        <ReviewRow label={<FormattedMessage id="setup.review.access" />}>
          {credentialsEnabled ? (
            input.initialUsername
          ) : (
            <FormattedMessage id="setup.review.no_credentials" />
          )}
        </ReviewRow>
        <ReviewRow label={<FormattedMessage id="setup.review.content_mode" />}>
          <FormattedMessage
            id={
              input.sfwContentMode
                ? "setup.review.sfw"
                : "setup.review.standard"
            }
          />
        </ReviewRow>
      </dl>
    </div>
  );
}

export function SetupWizard({
  status,
  onComplete,
}: {
  status: SystemStatus;
  onComplete: () => Promise<void> | void;
}) {
  const intl = useIntl();
  const [step, setStep] = useState(0);
  const [input, setInput] = useState(() => defaultSetupInput(status));
  const [emptyLibraryWarning, setEmptyLibraryWarning] = useState(false);
  const [error, setError] = useState("");
  const client = useApolloClient();
  const [setup, { loading }] = useMutation(GQL.SetupDocument);
  const { data: configurationData, loading: configurationLoading } = useQuery(
    GQL.ConfigurationDocument,
    { fetchPolicy: "network-only" },
  );
  const currentStep = STEPS[step];
  const overrides = configurationData?.configuration.general;
  const accessInvalid = useMemo(() => {
    const username = input.initialUsername ?? "";
    return (
      username.trim() !== username ||
      (Boolean(username) && !input.initialPassword)
    );
  }, [input.initialPassword, input.initialUsername]);

  function update(patch: Partial<GQL.SetupInput>) {
    setInput((current) => ({ ...current, ...patch }));
    setError("");
  }

  async function complete() {
    setError("");
    const variables: GQL.SetupMutationVariables = {
      input: {
        ...input,
        initialUsername: input.initialUsername || undefined,
        initialPassword: input.initialPassword || undefined,
        blobsLocation: input.storeBlobsInDatabase ? "" : input.blobsLocation,
      },
    };

    try {
      await setup({ variables });
      if (variables.input.initialUsername) {
        const loginURL = getPlatformURL("login");
        loginURL.searchParams.set("returnURL", window.location.href);
        window.location.assign(loginURL);
        return;
      }

      client.cache.evict({ id: "ROOT_QUERY", fieldName: "configuration" });
      client.cache.gc();
      await onComplete();
    } catch (setupError) {
      setError(
        setupError instanceof Error ? setupError.message : String(setupError),
      );
    }
  }

  function next() {
    if (
      currentStep === "storage" &&
      input.stashes.length === 0 &&
      !emptyLibraryWarning
    ) {
      setEmptyLibraryWarning(true);
      return;
    }
    if (currentStep === "review") {
      void complete();
      return;
    }
    setStep((current) => Math.min(current + 1, STEPS.length - 1));
  }

  if (configurationLoading && !configurationData) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <Spinner className="size-10 text-muted-foreground" />
      </div>
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/30 p-0 text-foreground sm:p-6">
      <div className="flex h-screen w-full max-w-5xl flex-col overflow-hidden bg-background sm:h-[min(46rem,calc(100dvh-3rem))] sm:rounded-lg sm:border sm:shadow-sm">
        <header className="border-b px-5 py-4 sm:px-8">
          <h1 className="text-lg font-semibold">
            <FormattedMessage id="setup.stash_setup_wizard" />
          </h1>
        </header>

        <div className="grid min-h-0 flex-1 md:grid-cols-[13rem_1fr]">
          <nav
            aria-label={intl.formatMessage({ id: "setup.progress" })}
            className="border-b bg-muted/20 p-4 md:border-r md:border-b-0 md:p-5"
          >
            <ol className="grid grid-cols-4 gap-2 md:grid-cols-1">
              {STEPS.map((item, index) => (
                <li
                  key={item}
                  className={`flex min-w-0 items-center gap-2 rounded-md px-2 py-2 text-sm ${
                    index === step
                      ? "bg-muted font-medium text-foreground"
                      : "text-muted-foreground"
                  }`}
                  aria-current={index === step ? "step" : undefined}
                >
                  <span className="flex size-5 shrink-0 items-center justify-center rounded-full border text-xs tabular-nums">
                    {index < step ? (
                      <CheckIcon className="size-3" />
                    ) : (
                      index + 1
                    )}
                  </span>
                  <span className="hidden truncate md:block">
                    <FormattedMessage id={`setup.steps.${item}`} />
                  </span>
                </li>
              ))}
            </ol>
          </nav>

          <ScrollArea className="min-h-0">
            <div className="mx-auto w-full max-w-2xl px-5 py-6 sm:px-8 sm:py-8">
              {currentStep === "configuration" && (
                <ConfigurationStep
                  status={status}
                  value={input.configLocation}
                  onChange={(configLocation) => update({ configLocation })}
                />
              )}
              {currentStep === "storage" && (
                <StorageStep
                  status={status}
                  input={input}
                  overrides={overrides}
                  onChange={update}
                  emptyLibraryWarning={emptyLibraryWarning}
                />
              )}
              {currentStep === "access" && (
                <AccessStep input={input} onChange={update} />
              )}
              {currentStep === "review" && (
                <ReviewStep
                  status={status}
                  input={input}
                  overrides={overrides}
                />
              )}

              {error && (
                <div
                  className="mt-6 space-y-2 rounded-lg border border-destructive/40 bg-destructive/5 p-4"
                  aria-live="polite"
                >
                  <p className="text-sm font-medium text-destructive">
                    <FormattedMessage id="setup.errors.something_went_wrong" />
                  </p>
                  <pre className="overflow-auto text-xs whitespace-pre-wrap">
                    {error}
                  </pre>
                </div>
              )}
            </div>
          </ScrollArea>
        </div>

        <SetupNavigation
          step={step}
          busy={loading}
          nextDisabled={currentStep === "access" && accessInvalid}
          onBack={() => {
            setError("");
            setStep((current) => Math.max(0, current - 1));
          }}
          onNext={next}
        />
      </div>
    </main>
  );
}
