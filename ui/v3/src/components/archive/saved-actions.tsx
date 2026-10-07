import { useCallback, useEffect, useId, useRef, useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import {
  createSavedActionsAPI,
  savedActionFamilySchema,
  type SavedActionsAPI,
  type SavedActionFamily,
  type SavedActionLocation,
  type SavedActionSummary,
} from "@/core/native-archive/saved-actions";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { useActivityRead } from "./activity/shared";
import { PostSection } from "./posts/shared";
import { useSavedActionLabels } from "./saved-actions-labels";

function ActionCard({
  item,
  family,
  busy,
  onResume,
  onInspect,
}: {
  item: SavedActionSummary;
  family: SavedActionFamily;
  busy: boolean;
  onResume: () => void;
  onInspect: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useSavedActionLabels();
  const readable = item.state !== "unreadable";
  return (
    <Card>
      <CardHeader className="gap-3">
        <CardTitle className="break-words">
          {item.label ?? labels.families[family]}
        </CardTitle>
        <div>
          <Badge variant={readable ? "secondary" : "destructive"}>
            {labels.states[item.state]}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {item.files !== undefined && (
          <p>
            {intl.formatMessage(
              {
                id: "saved_actions.files",
                defaultMessage:
                  "{count, plural, one {# file} other {# files}} in the saved batch",
              },
              { count: item.files },
            )}
          </p>
        )}
        {item.state === "rejected" && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "saved_actions.rejected_help",
              "The server rejected this request. Open its original review to make a new choice.",
            )}
          </p>
        )}
        {item.state === "unreadable" && (
          <p className="text-sm text-muted-foreground">
            {msg(
              "saved_actions.unreadable_help",
              "The saved record has been kept. Recovery is unavailable until it can be read.",
            )}
          </p>
        )}
        <PostSection title={msg("saved_actions.details", "Action details")}>
          <dl className="grid min-w-0 gap-2 text-sm">
            <dt className="text-muted-foreground">
              {msg("saved_actions.target_reference", "Target reference")}
            </dt>
            <dd className="break-all font-mono" data-selectable-text>
              {item.key}
            </dd>
          </dl>
        </PostSection>
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        <Button
          disabled={busy || !readable || item.state === "rejected"}
          onClick={onResume}
        >
          {msg("saved_actions.resume", "Resume saved action")}
        </Button>
        <Button
          variant="outline"
          disabled={busy || !readable}
          onClick={onInspect}
        >
          {msg("saved_actions.open_review", "Open original review")}
        </Button>
      </CardFooter>
    </Card>
  );
}

export function SavedActions({
  family,
  onFamilyChange,
  api: supplied,
}: {
  family: SavedActionFamily;
  onFamilyChange: (family: SavedActionFamily) => void;
  api?: SavedActionsAPI;
}) {
  const msg = useMsg();
  const labels = useSavedActionLabels();
  const navigate = useNavigate();
  const selectID = useId();
  const [defaultAPI] = useState(() => createSavedActionsAPI());
  const api = supplied ?? defaultAPI;
  const [cursors, setCursors] = useState([""]);
  const [refresh, setRefresh] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [checked, setChecked] = useState(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const after = cursors.at(-1) ?? "";
  const load = useCallback(
    (signal: AbortSignal) => api.page(family, after, signal),
    [api, family, after],
  );
  const read = useActivityRead(
    `${api.endpoint}:${family}:${after}`,
    load,
    refresh,
  );
  const options = savedActionFamilySchema.options.map((value) => ({
    value,
    label: labels.families[value],
  }));

  function open(location: SavedActionLocation) {
    switch (location.kind) {
      case "account":
        return navigate({
          to: "/account-review",
          search: {
            account: location.id,
            ownership: "all",
            q: "",
            namespace: "",
          },
        });
      case "post":
        return navigate({
          to: "/source-posts",
          search: { post: location.id, mode: "all", namespace: "", value: "" },
        });
      case "collection":
        return navigate({
          to: "/collections",
          search: {
            collection: location.id,
            create: location.create,
            q: "",
            state: "",
            kind: "",
          },
        });
      case "root":
        return navigate({
          to: "/media-roots",
          search: {
            root: location.id,
            create: location.create,
            q: "",
            state: "",
          },
        });
      case "scene":
        return navigate({
          to: "/scenes/$sceneId",
          params: { sceneId: location.id },
          search: { tab: location.tab },
        });
      case "image":
        return navigate({
          to: "/images/$imageId",
          params: { imageId: location.id },
          search: { tab: location.tab },
        });
    }
  }
  async function act(item: SavedActionSummary, inspect: boolean) {
    if (busy) return;
    setBusy(true);
    setError(undefined);
    setChecked(false);
    try {
      if (inspect) {
        const location = await api.location(family, item.key);
        if (alive.current) await open(location);
      } else {
        await api.recover(family, item.key);
        if (alive.current) setChecked(true);
      }
    } catch (error) {
      if (alive.current) setError(error);
    } finally {
      if (alive.current) {
        setBusy(false);
        setRefresh((value) => value + 1);
      }
    }
  }

  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 pb-24 md:p-6">
      <header className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">
          {msg("saved_actions.title", "Saved actions")}
        </h1>
        <p className="text-muted-foreground">
          {msg(
            "saved_actions.help",
            "Actions saved by this browser for this Stash installation. Resume checks the outcome and retries the original action when needed.",
          )}
        </p>
        <p className="text-sm text-muted-foreground">
          {msg(
            "saved_actions.scope",
            "This list covers this device. Current archive conflicts and server job progress are reviewed separately.",
          )}
        </p>
      </header>
      <div className="flex flex-wrap items-end gap-3">
        <Field className="min-w-0 flex-1">
          <FieldLabel htmlFor={selectID}>
            {msg("saved_actions.action_type", "Action type")}
          </FieldLabel>
          <Select
            items={options}
            value={family}
            disabled={busy}
            onValueChange={(value) =>
              onFamilyChange(savedActionFamilySchema.parse(value))
            }
          >
            <SelectTrigger id={selectID} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Button
          variant="outline"
          disabled={busy || read.busy}
          onClick={() => setRefresh((value) => value + 1)}
        >
          {msg("actions.refresh", "Refresh")}
        </Button>
      </div>
      {read.error !== undefined && (
        <ReviewError
          error={read.error}
          retry={() => setRefresh((value) => value + 1)}
        />
      )}
      {error !== undefined && <ReviewError error={error} />}
      {checked && (
        <Alert aria-live="polite">
          <AlertTitle>
            {msg("saved_actions.checked", "Saved action checked")}
          </AlertTitle>
          <AlertDescription>
            <p>
              {msg(
                "saved_actions.work_continues",
                "Queued work may still be running. Archive activity shows server job progress.",
              )}
            </p>
            <Link
              className={buttonVariants({ variant: "outline", size: "sm" })}
              to="/archive-activity"
              search={{ view: "jobs", kind: "", state: "", collection: "" }}
            >
              {msg("archive_activity.title", "Archive activity")}
            </Link>
          </AlertDescription>
        </Alert>
      )}
      {(read.busy || busy) && (
        <div className="flex items-center gap-2" role="status">
          <Spinner />
          {msg("saved_actions.loading", "Checking saved actions…")}
        </div>
      )}
      {read.current?.items.length === 0 && (
        <p>{msg("saved_actions.empty", "No saved actions on this page.")}</p>
      )}
      <div className="grid min-w-0 gap-4 md:grid-cols-2">
        {read.current?.items.map((item) => (
          <ActionCard
            key={item.key}
            item={item}
            family={family}
            busy={busy}
            onResume={() => void act(item, false)}
            onInspect={() => void act(item, true)}
          />
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={busy || read.busy || cursors.length < 2}
          onClick={() => setCursors((values) => values.slice(0, -1))}
        >
          {msg("actions.previous", "Previous")}
        </Button>
        <Button
          variant="outline"
          disabled={busy || read.busy || !read.current?.next}
          onClick={() => {
            const next = read.current?.next;
            if (next) setCursors((values) => [...values, next]);
          }}
        >
          {msg("actions.next", "Next")}
        </Button>
      </div>
    </main>
  );
}
