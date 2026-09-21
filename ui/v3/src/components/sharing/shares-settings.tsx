import { useEffect, useId, useState } from "react";
import { useMutation, useQuery } from "@apollo/client/react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { Eye, Link, Pencil, Plus, RotateCcw, ShieldOff } from "lucide-react";
import * as GQL from "@/core/generated-graphql";
import { SettingsSection } from "@/components/settings/setting-row";
import { EntityActionsMenu } from "@/components/detail/entity-actions-menu";
import { DestructiveConfirmDialog } from "@/components/shared/destructive-confirm-dialog";
import { QueryError } from "@/components/query-error";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { useMsg } from "@/hooks/message";
import { useToast } from "@/hooks/toast";
import { ShareDialog } from "./share-dialog";
import { ShareLinkDialog, useSharePreview } from "./share-link-dialog";

function ShareAddress({ value }: { value: string }) {
  const msg = useMsg();
  const toast = useToast();
  const id = useId();
  const [save] = useMutation(GQL.ConfigureSharingDocument, {
    refetchQueries: [GQL.MediaSharesDocument],
  });
  const form = useForm({
    defaultValues: { address: value },
    validators: {
      onChange: z.object({
        address: z.string().refine(
          (address) => {
            if (!address.trim()) return true;
            try {
              const url = new URL(address);
              return (
                url.protocol === "https:" &&
                !url.username &&
                !url.password &&
                !url.search &&
                !url.hash &&
                url.pathname.replace(/\/$/, "").endsWith("/share")
              );
            } catch {
              return false;
            }
          },
          msg(
            "sharing.address_invalid",
            "Use an HTTPS address ending in /share, or leave blank.",
          ),
        ),
      }),
    },
    onSubmit: async ({ value: next }) => {
      try {
        await save({ variables: { public_url: next.address.trim() } });
        toast.success(msg("sharing.saved", "Share updated"));
      } catch (error) {
        toast.error(error);
      }
    },
  });
  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <form.Field name="address">
        {(field) => (
          <Field data-invalid={field.state.meta.errors.length > 0}>
            <FieldLabel htmlFor={id}>
              {msg("sharing.public_address", "Public share address")}
            </FieldLabel>
            <Input
              id={id}
              type="url"
              value={field.state.value}
              onChange={(event) => field.handleChange(event.target.value)}
              onBlur={field.handleBlur}
              placeholder="https://nsfw.ak.codes/share"
            />
            <FieldDescription>
              {msg(
                "sharing.address_description",
                "Use the address served by your sharing proxy. Leave blank to use this server’s /share path. Changing this setting affects newly generated links.",
              )}
            </FieldDescription>
            <FieldError errors={field.state.meta.errors} />
          </Field>
        )}
      </form.Field>
      <form.Subscribe
        selector={(state) => [
          state.canSubmit,
          state.isSubmitting,
          state.isDirty,
        ]}
      >
        {([canSubmit, submitting, dirty]) => (
          <Button
            type="submit"
            variant="outline"
            disabled={!canSubmit || submitting || !dirty}
          >
            {msg("actions.save", "Save")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

function ShareCard({
  share,
  refresh,
}: {
  share: GQL.MediaShareFieldsFragment;
  refresh: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const toast = useToast();
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"revoke" | "rotate">();
  const [link, setLink] = useState<{ id: string; url: string }>();
  const [revoke, { loading: revoking }] = useMutation(
    GQL.MediaShareRevokeDocument,
  );
  const [rotate, { loading: rotating }] = useMutation(
    GQL.MediaShareRotateDocument,
  );
  const preview = useSharePreview();
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  const active =
    !share.revoked_at && new Date(share.expires_at).getTime() > now;
  async function confirmAction() {
    try {
      if (confirm === "revoke") await revoke({ variables: { id: share.id } });
      else if (confirm === "rotate") {
        const result = await rotate({ variables: { id: share.id } });
        if (result.data)
          setLink({ id: share.id, url: result.data.mediaShareRotate });
      }
      setConfirm(undefined);
      refresh();
    } catch (error) {
      toast.error(error);
    }
  }
  return (
    <>
      <Card>
        <CardHeader className="flex-row items-start justify-between gap-3">
          <div className="min-w-0 space-y-2">
            <CardTitle className="break-words">{share.label}</CardTitle>
            <CardDescription>
              {intl.formatMessage(
                {
                  id: "sharing.media_count",
                  defaultMessage:
                    "{count, plural, one {# media item} other {# media items}}",
                },
                { count: share.media_count },
              )}
            </CardDescription>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Badge variant={active ? "secondary" : "outline"}>
              {share.revoked_at
                ? msg("sharing.revoked", "Revoked")
                : active
                  ? msg("sharing.active", "Active")
                  : msg("sharing.expired", "Expired")}
            </Badge>
            <EntityActionsMenu
              busy={revoking || rotating || preview.loading}
              items={[
                {
                  key: "preview",
                  icon: Eye,
                  label: msg("sharing.preview", "Recipient preview"),
                  onSelect: () => void preview.open(share.id),
                  disabled: !active,
                },
                {
                  key: "edit",
                  icon: Pencil,
                  label: msg("sharing.edit", "Edit share"),
                  onSelect: () => setEditing(true),
                  disabled: !!share.revoked_at,
                },
                {
                  key: "rotate",
                  icon: RotateCcw,
                  label: msg("sharing.regenerate", "Regenerate link"),
                  onSelect: () => setConfirm("rotate"),
                  disabled: !active,
                },
                {
                  key: "revoke",
                  icon: ShieldOff,
                  label: msg("sharing.revoke", "Revoke share"),
                  onSelect: () => setConfirm("revoke"),
                  disabled: !!share.revoked_at,
                  destructive: true,
                },
              ]}
            />
          </div>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <div className="flex flex-wrap gap-x-6 gap-y-2 text-muted-foreground">
            <span>
              {intl.formatMessage(
                { id: "sharing.expires_on", defaultMessage: "Expires {date}" },
                {
                  date: intl.formatDate(share.expires_at, {
                    dateStyle: "medium",
                    timeStyle: "short",
                  }),
                },
              )}
            </span>
            <span>
              {intl.formatMessage(
                {
                  id: "sharing.openings",
                  defaultMessage:
                    "{count, plural, one {# link opening} other {# link openings}}",
                },
                { count: share.access_count },
              )}
            </span>
            {share.last_accessed_at && (
              <span>
                {intl.formatMessage(
                  {
                    id: "sharing.last_opened",
                    defaultMessage: "Last opened {date}",
                  },
                  {
                    date: intl.formatDate(share.last_accessed_at, {
                      dateStyle: "medium",
                      timeStyle: "short",
                    }),
                  },
                )}
              </span>
            )}
          </div>
          <div className="flex flex-wrap gap-2">
            {share.show_metadata && (
              <Badge variant="outline">
                {msg("sharing.show_titles", "Show titles")}
              </Badge>
            )}
            {share.allow_download && (
              <Badge variant="outline">
                {msg("sharing.allow_originals", "Allow original downloads")}
              </Badge>
            )}
          </div>
        </CardContent>
      </Card>
      <ShareDialog open={editing} onOpenChange={setEditing} share={share} />
      {confirm && (
        <DestructiveConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirm(undefined);
          }}
          title={
            confirm === "revoke"
              ? msg("sharing.revoke", "Revoke share")
              : msg("sharing.regenerate", "Regenerate link")
          }
          onConfirm={() => {
            if (!revoking && !rotating) void confirmAction();
          }}
        >
          <p className="text-sm">
            {confirm === "revoke"
              ? msg(
                  "sharing.revoke_description",
                  "The link and every active session will stop working. Media already saved by recipients cannot be removed.",
                )
              : msg(
                  "sharing.rotate_description",
                  "The old link and every active session will stop working. You will receive a new link to copy.",
                )}
          </p>
        </DestructiveConfirmDialog>
      )}
      {link && (
        <ShareLinkDialog link={link} onClose={() => setLink(undefined)} />
      )}
    </>
  );
}

export function SharesSettings() {
  const msg = useMsg();
  const [offset, setOffset] = useState(0);
  const [creating, setCreating] = useState(false);
  const { data, error, loading, refetch } = useQuery(GQL.MediaSharesDocument, {
    variables: { limit: 50, offset },
    fetchPolicy: "cache-and-network",
    pollInterval: 30_000,
  });
  return (
    <div className="max-w-4xl space-y-8 p-6">
      <SettingsSection
        title={msg("sharing.shares", "Shares")}
        description={msg(
          "sharing.description",
          "Manage anonymous, expiring access to selected media. Revoke access at any time.",
        )}
      >
        <div>
          <Button onClick={() => setCreating(true)}>
            <Plus />
            {msg("sharing.create", "Create share")}
          </Button>
        </div>
        {error && (
          <QueryError
            error={error}
            retry={refetch}
            retrying={loading}
            stale={!!data}
          />
        )}
        {loading && !data && <Spinner />}
        {data?.mediaShares.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Link />
              </EmptyMedia>
              <EmptyTitle>{msg("sharing.empty", "No shares yet")}</EmptyTitle>
              <EmptyDescription>
                {msg(
                  "sharing.empty_description",
                  "Create a share here, from a media page, or from a selection in your library.",
                )}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <div className="space-y-4">
          {data?.mediaShares.map((share) => (
            <ShareCard
              key={share.id}
              share={share}
              refresh={() => void refetch()}
            />
          ))}
        </div>
        {(offset > 0 || data?.mediaShares.length === 50) && (
          <div className="flex justify-between gap-2">
            <Button
              variant="outline"
              disabled={offset === 0 || loading}
              onClick={() => setOffset((value) => Math.max(0, value - 50))}
            >
              {msg("actions.previous", "Previous")}
            </Button>
            <Button
              variant="outline"
              disabled={data?.mediaShares.length !== 50 || loading}
              onClick={() => setOffset((value) => value + 50)}
            >
              {msg("actions.next", "Next")}
            </Button>
          </div>
        )}
      </SettingsSection>
      {data && (
        <SettingsSection title={msg("sharing.delivery", "Share delivery")}>
          <ShareAddress
            key={data.sharingConfiguration.public_url}
            value={data.sharingConfiguration.public_url}
          />
        </SettingsSection>
      )}
      <ShareDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}
