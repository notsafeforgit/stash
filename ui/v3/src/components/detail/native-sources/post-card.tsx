import { useId, useState } from "react";
import { useIntl } from "react-intl";
import { useForm } from "@tanstack/react-form";
import { Link } from "@tanstack/react-router";
import { z } from "zod";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  sourceLinkStateSchema,
  type SourceAssociation,
  type SourceLinkState,
  type SourcePost,
  type SourceReviewAPI,
} from "@/core/native-archive/source-review-api";
import { AccountService } from "@/components/archive/accounts/shared";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
} from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ReviewError } from "../native-metadata/shared";
import {
  CaptureTime,
  LinkState,
  SourceCaptures,
  SourceLinkHistory,
} from "./history";

function SourceLinkForm({
  association,
  blocked,
  apply,
}: {
  association: SourceAssociation;
  blocked: boolean;
  apply: (
    association: SourceAssociation,
    state: SourceLinkState,
    reason: string,
  ) => Promise<void>;
}) {
  const msg = useMsg();
  const id = useId();
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: { state: "" as "" | SourceLinkState, reason: "" },
    validators: {
      onChange: z.object({
        state: sourceLinkStateSchema,
        reason: z
          .string()
          .refine((v) => new TextEncoder().encode(v).length <= 4096),
      }),
    },
    onSubmit: async ({ value }) => {
      setError(undefined);
      try {
        await apply(
          association,
          sourceLinkStateSchema.parse(value.state),
          value.reason,
        );
      } catch (error) {
        setError(error);
      }
    },
  });
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      {error !== undefined && <ReviewError error={error} />}
      <FieldGroup>
        <form.Field name="state">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-choice`}>
                {msg("source_review.choice", "Post link")}
              </FieldLabel>
              <ToggleGroup
                variant="outline"
                multiple={false}
                disabled={blocked}
                aria-labelledby={`${id}-choice`}
                value={field.state.value ? [field.state.value] : []}
                onValueChange={(value) => field.handleChange(value[0] ?? "")}
              >
                <ToggleGroupItem value="linked">
                  {msg("source_review.link", "Link")}
                </ToggleGroupItem>
                <ToggleGroupItem value="unlinked">
                  {msg("source_review.unlink", "Unlink")}
                </ToggleGroupItem>
                <ToggleGroupItem value="undecided">
                  {msg("source_review.attachments", "Use attachments")}
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {field.state.value === "linked"
                  ? msg(
                      "source_review.link_help",
                      "Allow this post's captures as metadata sources for this item. This does not choose an attachment or create an album.",
                    )
                  : field.state.value === "unlinked"
                    ? msg(
                        "source_review.unlink_help",
                        "Exclude this post as a source for this item, including its attachment links. Existing source albums may remove automatic membership; manual gallery choices are kept.",
                      )
                    : msg(
                        "source_review.attachments_help",
                        "Clear the explicit post choice and use the existing attachment links. Retained file evidence alone does not select a link.",
                      )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Field name="reason">
          {(field) => {
            const invalid = !field.state.meta.isValid;
            return (
              <Field data-disabled={blocked} data-invalid={invalid}>
                <FieldLabel htmlFor={`${id}-reason`}>
                  {msg("source_review.reason", "Reason (optional)")}
                </FieldLabel>
                <Textarea
                  id={`${id}-reason`}
                  value={field.state.value}
                  disabled={blocked}
                  maxLength={4096}
                  aria-invalid={invalid}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
                {invalid && (
                  <FieldDescription>
                    {msg(
                      "source_review.reason_length",
                      "Shorten the reason before saving.",
                    )}
                  </FieldDescription>
                )}
              </Field>
            );
          }}
        </form.Field>
      </FieldGroup>
      <p className="text-sm text-muted-foreground">
        {msg(
          "source_review.metadata_kept",
          "Saved titles, performers and other metadata choices are kept when the source link changes.",
        )}
      </p>
      <form.Subscribe
        selector={(state) =>
          [state.canSubmit, state.isSubmitting, state.values.state] as const
        }
      >
        {([canSubmit, submitting, state]) => (
          <Button
            type="submit"
            disabled={blocked || !state || !canSubmit || submitting}
          >
            {msg("source_review.save", "Save source link")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

function SourceURL({ value }: { value: string }) {
  let href: string | undefined;
  try {
    const url = new URL(value);
    if (/^https?:$/.test(url.protocol) && !url.username && !url.password)
      href = url.href;
  } catch {
    /* Retained non-web values remain readable without becoming links. */
  }
  return href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      data-selectable-text
      className="wrap-anywhere text-sm underline underline-offset-4"
    >
      {value}
    </a>
  ) : (
    <span data-selectable-text className="wrap-anywhere text-sm">
      {value}
    </span>
  );
}

export function SourcePostCard({
  post,
  api,
  blocked,
  apply,
}: {
  post: SourcePost;
  api: SourceReviewAPI;
  blocked: boolean;
  apply: (
    association: SourceAssociation,
    state: SourceLinkState,
    reason: string,
  ) => Promise<void>;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [reviewOpen, setReviewOpen] = useState(false);
  const [capturesOpen, setCapturesOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [urls, setURLs] = useState(post.urls);
  const [moreURLs, setMoreURLs] = useState(post.more_urls);
  const [loadingURLs, setLoadingURLs] = useState(false);
  const [error, setError] = useState<unknown>();
  const a = post.association;
  const available = a.post_state === "active" && a.media_state === "active";
  async function loadURLs() {
    setLoadingURLs(true);
    setError(undefined);
    try {
      const rows = await api.urls(a.post_uuid, urls.at(-1)?.uuid);
      setURLs((prior) => [...prior, ...rows]);
      setMoreURLs(rows.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setLoadingURLs(false);
    }
  }
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="wrap-anywhere" data-selectable-text>
          {post.latest_capture?.title ||
            msg("source_review.untitled", "Untitled source post")}
          {post.latest_capture?.title_truncated ? "…" : ""}
        </CardTitle>
        <CardDescription>
          {post.latest_capture ? (
            <CaptureTime capture={post.latest_capture} />
          ) : (
            msg(
              "source_review.no_captures",
              "No retained captures for this post.",
            )
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          {post.latest_capture && (
            <AccountService
              namespace={`native:${post.latest_capture.platform}`}
            />
          )}
          <Badge variant={a.state === "conflict" ? "destructive" : "outline"}>
            <LinkState state={a.state} attachments={post.linked_attachments} />
          </Badge>
          {!available && (
            <Badge variant="secondary">
              {msg("source_review.unavailable", "Source or item is retired")}
            </Badge>
          )}
        </div>
        {post.linked_attachments > 0 && (
          <p className="text-sm text-muted-foreground">
            {intl.formatMessage(
              {
                id: "source_review.attachment_count",
                defaultMessage:
                  "{count, plural, one {# retained attachment link} other {# retained attachment links}}",
              },
              { count: post.linked_attachments },
            )}
          </p>
        )}
        {post.has_retained_evidence &&
          a.state === "undecided" &&
          post.linked_attachments === 0 && (
            <p className="text-sm text-muted-foreground">
              {msg(
                "source_review.evidence_help",
                "Retained catalog evidence mentions this item. Review it before selecting a source link.",
              )}
            </p>
          )}
        <ul className="flex flex-col gap-2">
          {urls.map((url) => (
            <li key={url.uuid}>
              <SourceURL value={url.url} />
            </li>
          ))}
        </ul>
        {moreURLs && (
          <Button
            variant="outline"
            disabled={loadingURLs}
            onClick={() => void loadURLs()}
          >
            {msg("source_review.more_urls", "Load more source URLs")}
          </Button>
        )}
        {error !== undefined && (
          <ReviewError error={error} retry={() => void loadURLs()} />
        )}
        <Collapsible open={capturesOpen} onOpenChange={setCapturesOpen}>
          <CollapsibleTrigger
            render={
              <Button variant="ghost" className="w-full justify-between" />
            }
          >
            {msg("source_review.captures", "Post text and captures")}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-3">
            {capturesOpen && <SourceCaptures api={api} post={a.post_uuid} />}
          </CollapsibleContent>
        </Collapsible>
        <Collapsible open={historyOpen} onOpenChange={setHistoryOpen}>
          <CollapsibleTrigger
            render={
              <Button variant="ghost" className="w-full justify-between" />
            }
          >
            {msg("source_review.history", "Link history")}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-3">
            {historyOpen && (
              <SourceLinkHistory
                api={api}
                post={a.post_uuid}
                media={a.media_uuid}
              />
            )}
          </CollapsibleContent>
        </Collapsible>
      </CardContent>
      <CardFooter className="flex-col items-stretch gap-3">
        <Link
          className={buttonVariants({ variant: "outline" })}
          to="/source-posts"
          search={{ post: a.post_uuid }}
        >
          {msg("source_posts.open", "Open post")}
        </Link>
        <Collapsible
          className="w-full"
          open={reviewOpen}
          onOpenChange={setReviewOpen}
        >
          <CollapsibleTrigger
            render={
              <Button
                variant="outline"
                className="w-full justify-between"
                disabled={blocked || !available}
              />
            }
          >
            {msg("source_review.review_link", "Review source link")}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-3">
            {reviewOpen && (
              <SourceLinkForm
                association={a}
                blocked={blocked || !available}
                apply={apply}
              />
            )}
          </CollapsibleContent>
        </Collapsible>
      </CardFooter>
    </Card>
  );
}
