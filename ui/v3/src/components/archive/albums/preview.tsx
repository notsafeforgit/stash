import { useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import {
  albumPolicySchema,
  type AlbumIdentity,
  type AlbumPolicy,
  type AlbumMatch,
  type AlbumPreview,
  type AlbumReviewAPI,
} from "@/core/native-archive/album-review-api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardDescription,
} from "@/components/ui/card";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { PostSection } from "../posts/shared";
import {
  AlbumItemLink,
  AlbumReviewError,
  useAlbumLabels,
} from "./review-shared";

function Members({ items }: { items: AlbumIdentity[] }) {
  const msg = useMsg();
  const [limit, setLimit] = useState(25);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        {items.slice(0, limit).map((item) => (
          <AlbumItemLink key={item.uuid} item={item} />
        ))}
      </div>
      {limit < items.length && (
        <Button
          type="button"
          variant="outline"
          onClick={() => setLimit((n) => n + 25)}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
function MatchProofs({ match }: { match: AlbumMatch }) {
  const msg = useMsg();
  const labels = useAlbumLabels();
  const [limit, setLimit] = useState(25);
  const proofs = match.candidates.flatMap((candidate) =>
    candidate.proofs.map((proof) => ({ candidate, proof })),
  );
  return (
    <div className="flex flex-col gap-3">
      {proofs.slice(0, limit).map(({ candidate, proof }) => (
        <Card key={`${candidate.media_uuid}:${proof.evidence_uuid}`} size="sm">
          <CardHeader>
            <CardDescription>{labels.basis[proof.basis]}</CardDescription>
            <CardTitle>{labels.proof[proof.status]}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-2 text-sm">
            {proof.relative_path && (
              <p className="wrap-anywhere" data-selectable-text>
                {proof.relative_path}
              </p>
            )}
            <p className="text-muted-foreground">
              {msg("album_review.candidate_identity", "Library item identity")}
            </p>
            <code className="wrap-anywhere text-xs" data-selectable-text>
              {candidate.media_uuid}
            </code>
          </CardContent>
        </Card>
      ))}
      {limit < proofs.length && (
        <Button
          type="button"
          variant="outline"
          onClick={() => setLimit((n) => n + 25)}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
function Matches({ matches }: { matches: AlbumMatch[] }) {
  const msg = useMsg();
  const labels = useAlbumLabels();
  const [limit, setLimit] = useState(25);
  return (
    <div className="flex flex-col gap-3">
      {matches.slice(0, limit).map((match) => (
        <Card key={match.attachment_uuid} size="sm">
          <CardHeader>
            <CardTitle>{labels.match[match.status]}</CardTitle>
            <CardDescription className="wrap-anywhere" data-selectable-text>
              {match.reference.namespace}:{match.reference.value}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm">
            {match.reason && (
              <p className="text-muted-foreground">
                {labels.reason[match.reason as keyof typeof labels.reason] ??
                  msg(
                    "album_review.reason_unknown",
                    "This candidate needs additional review before it can be linked.",
                  )}
              </p>
            )}
            {match.status === "preserved" && (
              <p className="text-muted-foreground">
                {msg(
                  "album_review.preserved_help",
                  "The saved attachment choice stays unchanged, including rejected or undecided links.",
                )}
              </p>
            )}
            {match.status === "unavailable" && (
              <p className="text-muted-foreground">
                {msg(
                  "album_review.unavailable_help",
                  "No current library file can be matched from this evidence. This does not establish whether a download is pending or has failed.",
                )}
              </p>
            )}
            {match.candidates.length > 0 && (
              <PostSection
                title={msg("album_review.proofs", "Inspect matching evidence")}
              >
                <MatchProofs match={match} />
              </PostSection>
            )}
          </CardContent>
        </Card>
      ))}
      {limit < matches.length && (
        <Button
          type="button"
          variant="outline"
          onClick={() => setLimit((n) => n + 25)}
        >
          {msg("source_posts.load_more", "Load more")}
        </Button>
      )}
    </div>
  );
}
function PreviewResult({ preview }: { preview: AlbumPreview }) {
  const msg = useMsg(),
    intl = useIntl();
  const labels = useAlbumLabels();
  const counts = { matched: 0, preserved: 0, review: 0, unavailable: 0 };
  for (const match of preview.matches)
    counts[match.status === "ambiguous" ? "review" : match.status]++;
  return (
    <Card data-album-preview>
      <CardHeader>
        <CardTitle>{labels.action[preview.action]}</CardTitle>
        <CardDescription>
          {msg(
            "album_review.preview_only",
            "Preview only. No library data has changed.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap gap-2">
          {Object.entries(counts).map(([kind, count]) => (
            <Badge key={kind} variant="outline">
              {intl.formatMessage(
                {
                  id: "album_review.count",
                  defaultMessage: "{label}: {count, number}",
                },
                { label: labels.match[kind as keyof typeof counts], count },
              )}
            </Badge>
          ))}
        </div>
        <p className="text-sm text-muted-foreground">
          {msg(
            "album_review.match_help",
            "Only unique matches supported by a current file will be selected. Existing choices, manual gallery exclusions and unresolved candidates are preserved.",
          )}
        </p>
        {preview.gallery && <AlbumItemLink item={preview.gallery} />}
        {preview.initial_metadata && (
          <PostSection
            title={msg("album_review.initial_metadata", "New gallery metadata")}
          >
            <dl className="grid gap-2 text-sm">
              <dt className="text-muted-foreground">{msg("title", "Title")}</dt>
              <dd className="wrap-anywhere" data-selectable-text>
                {preview.initial_metadata.title ||
                  msg("archive_review.empty_value", "No value")}
              </dd>
              <dt className="text-muted-foreground">
                {msg("details", "Details")}
              </dt>
              <dd
                className="max-h-64 overflow-y-auto whitespace-pre-wrap wrap-anywhere"
                data-selectable-text
              >
                {preview.initial_metadata.details ||
                  msg("archive_review.empty_value", "No value")}
              </dd>
              <dt className="text-muted-foreground">{msg("date", "Date")}</dt>
              <dd data-selectable-text>
                {preview.initial_metadata.date ||
                  msg("archive_review.empty_value", "No value")}
              </dd>
            </dl>
          </PostSection>
        )}
        {preview.add.length > 0 && (
          <PostSection
            title={intl.formatMessage(
              {
                id: "album_review.add_members",
                defaultMessage: "Add to gallery ({count, number})",
              },
              { count: preview.add.length },
            )}
          >
            <Members items={preview.add} />
          </PostSection>
        )}
        {preview.remove.length > 0 && (
          <PostSection
            title={intl.formatMessage(
              {
                id: "album_review.remove_members",
                defaultMessage: "Remove from gallery ({count, number})",
              },
              { count: preview.remove.length },
            )}
          >
            <Members items={preview.remove} />
          </PostSection>
        )}
        {preview.matches.length > 0 && (
          <PostSection
            title={msg("album_review.attachments", "Attachment choices")}
          >
            <Matches matches={preview.matches} />
          </PostSection>
        )}
      </CardContent>
    </Card>
  );
}

const formSchema = z.object({ policy: albumPolicySchema });
export function AlbumPreviewForm({
  post,
  api,
  disabled,
  onApply,
}: {
  post: string;
  api: AlbumReviewAPI;
  disabled: boolean;
  onApply: (preview: AlbumPreview) => Promise<void>;
}) {
  const msg = useMsg(),
    labels = useAlbumLabels(),
    id = useId();
  const [preview, setPreview] = useState<AlbumPreview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: { policy: "source-identifiers-v1" as AlbumPolicy },
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      setBusy(true);
      setError(undefined);
      setPreview(undefined);
      try {
        setPreview(await api.preview(post, value.policy));
      } catch (error) {
        setError(error);
      } finally {
        setBusy(false);
      }
    },
  });
  const blocked = disabled || busy;
  const applicable =
    preview &&
    (preview.action === "create" || preview.action === "sync") &&
    (preview.action === "create" ||
      preview.add.length > 0 ||
      preview.remove.length > 0 ||
      preview.matches.some((match) => match.status === "matched"));
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!blocked) void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.Field name="policy">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-policy`}>
                {msg("album_review.policy", "Match existing media using")}
              </FieldLabel>
              <ToggleGroup
                variant="outline"
                aria-labelledby={`${id}-policy`}
                value={[field.state.value]}
                disabled={blocked}
                onValueChange={(values) => {
                  if (values[0]) {
                    field.handleChange(values[0]);
                    setPreview(undefined);
                  }
                }}
              >
                {albumPolicySchema.options.map((policy) => (
                  <ToggleGroupItem key={policy} value={policy}>
                    {labels.policy[policy]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <FieldDescription>
                {field.state.value === "source-identifiers-v1"
                  ? msg(
                      "album_review.source_ids_help",
                      "Match qualified source media IDs against retained evidence and verified library files.",
                    )
                  : msg(
                      "album_review.reddit_names_help",
                      "Also recognize original Reddit file names containing both the post ID and media ID. Conflicting explicit IDs never fall back to a file name.",
                    )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      {error !== undefined && <AlbumReviewError error={error} />}
      <Button type="submit" variant="outline" disabled={blocked}>
        {busy && <Spinner data-icon="inline-start" />}
        {msg("album_review.preview", "Preview album changes")}
      </Button>
      {preview && <PreviewResult preview={preview} />}
      {applicable && (
        <Button
          type="button"
          disabled={blocked}
          onClick={() => {
            if (!preview || blocked) return;
            setBusy(true);
            void onApply(preview)
              .catch(setError)
              .finally(() => setBusy(false));
          }}
        >
          {msg("album_review.apply", "Apply reviewed album changes")}
        </Button>
      )}
      {preview?.action === "sync" && !applicable && (
        <p className="text-sm text-muted-foreground">
          {msg(
            "album_review.no_changes",
            "No automatic changes are needed for this preview.",
          )}
        </p>
      )}
    </form>
  );
}
