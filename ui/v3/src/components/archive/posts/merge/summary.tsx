import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  PostMergePost,
  PostMergePreview,
  PostMergeReceipt,
} from "@/core/native-archive/post-consolidation-schema";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { PostSection, PostURL } from "../shared";
import {
  MergeEntity,
  MergeList,
  MergePostLink,
  attachmentKey,
  useMergeLabels,
} from "./shared";

export function MergeOriginals({ posts }: { posts: PostMergePost[] }) {
  const msg = useMsg();
  const labels = useMergeLabels();
  return (
    <PostSection
      title={msg("post_merge.originals", "Original posts and saved choices")}
    >
      <MergeList items={posts} rowKey={(post) => post.uuid}>
        {(post) => (
          <Card>
            <CardHeader>
              <CardTitle className="wrap-anywhere" data-selectable-text>
                {labels.name(post)}
              </CardTitle>
              <CardDescription className="wrap-anywhere" data-selectable-text>
                {post.identifiers
                  .map((i) => `${i.namespace} · ${i.value}`)
                  .join("; ")}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <MergePostLink id={post.uuid}>
                {msg("source_posts.open", "Open post")}
              </MergePostLink>
              {post.urls.map((url) => (
                <PostURL key={url} value={url} />
              ))}
              {post.selection ? (
                <>
                  <p>
                    {msg("post_merge.original_list", "Saved source list")}:{" "}
                    {labels.selection[post.selection.mode]}
                  </p>
                  <PostSection
                    title={msg("source_albums.order", "Source order")}
                  >
                    <MergeList
                      items={post.selection.entries}
                      rowKey={(entry) => String(entry.position)}
                    >
                      {(entry) => {
                        const attachment = post.attachments.find(
                          (item) => item.uuid === entry.attachment_uuid,
                        );
                        return (
                          <p
                            data-selectable-text
                            className="wrap-anywhere text-sm"
                          >
                            {entry.position + 1} ·{" "}
                            {attachment
                              ? `${attachment.namespace} · ${attachment.value}`
                              : entry.attachment_uuid}
                          </p>
                        );
                      }}
                    </MergeList>
                  </PostSection>
                </>
              ) : (
                <p>
                  {msg(
                    "post_merge.no_source_list",
                    "No saved source-list choice",
                  )}
                </p>
              )}
              {post.album?.gallery && <MergeEntity item={post.album.gallery} />}
              {post.album?.state === "disabled" && (
                <p>
                  {msg(
                    "association_review.disabled",
                    "Gallery association disabled",
                  )}
                </p>
              )}
              <PostSection
                title={msg("source_posts.media", "Media associations")}
              >
                <MergeList
                  items={post.media_choices}
                  rowKey={(item) => item.media.uuid}
                >
                  {(item) => (
                    <div className="flex flex-col gap-1">
                      <MergeEntity item={item.media} />
                      <p>{labels.state[item.decision.state]}</p>
                    </div>
                  )}
                </MergeList>
                <MergeList
                  items={post.attachments}
                  rowKey={(item) => item.uuid}
                >
                  {(item) => (
                    <div className="flex flex-col gap-1">
                      <code
                        data-selectable-text
                        className="wrap-anywhere text-sm"
                      >
                        {item.namespace} · {item.value}
                      </code>
                      <p>
                        {item.choice
                          ? labels.state[item.choice.state]
                          : msg(
                              "association_review.no_choice",
                              "No saved association choice",
                            )}
                      </p>
                      {item.media && <MergeEntity item={item.media} />}
                    </div>
                  )}
                </MergeList>
              </PostSection>
            </CardContent>
          </Card>
        )}
      </MergeList>
    </PostSection>
  );
}

export function MergePlan({ preview }: { preview: PostMergePreview }) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useMergeLabels();
  const blockers = {
    source_list_choice: msg(
      "post_merge.block_source_list",
      "Choose a source list or explicitly combine compatible lists.",
    ),
    source_list_conflict: msg(
      "post_merge.block_source_conflict",
      "These lists disagree about source positions. Choose one list.",
    ),
    gallery_choice: msg(
      "post_merge.block_gallery",
      "Choose which gallery association to retain, or disable it.",
    ),
    gallery_unavailable: msg(
      "post_merge.block_gallery_missing",
      "The selected gallery is unavailable. Choose another or disable the association.",
    ),
    gallery_requires_review: msg(
      "post_merge.block_gallery_review",
      "The selected gallery cannot be managed by this post. Choose another or disable the association.",
    ),
    gallery_claimed: msg(
      "post_merge.block_gallery_claimed",
      "Another post claims this gallery. Resolve that association, choose another, or disable it.",
    ),
    media_choice: msg(
      "post_merge.block_media",
      "Choose the resulting post link for this media.",
    ),
    media_unavailable: msg(
      "post_merge.block_media_missing",
      "This media cannot be newly linked. Keep it unlinked or unselected.",
    ),
    attachment_choice: msg(
      "post_merge.block_attachment",
      "Choose the resulting link for this attachment.",
    ),
    attachment_post_unlink: msg(
      "post_merge.block_post_unlink",
      "This attachment links to media whose post link is rejected. Resolve both choices together.",
    ),
  };
  const statuses = {
    unselected: msg("source_albums.unselected", "No selected library item"),
    unlinked: msg("source_albums.unlinked", "Attachment link rejected"),
    deleted: msg(
      "association_review.deleted_item",
      "This library item was deleted.",
    ),
    linked: msg("association_review.linked", "Linked"),
    post_unlinked: msg("source_albums.post_unlinked", "Post link rejected"),
    post_undecided: msg("source_review.undecided", "No selected link"),
    post_conflict: msg("source_albums.post_conflict", "Conflicting post links"),
    excluded: msg("source_albums.excluded", "Manually excluded from gallery"),
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle>{msg("post_merge.proposed", "Proposed result")}</CardTitle>
        <CardDescription>
          {msg(
            "post_merge.preview_help",
            "This preview has not changed the archive. Applying saves the selected links and gallery membership together with the post merge.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {preview.blockers.length > 0 && (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "post_merge.resolve",
                "Resolve these choices before merging",
              )}
            </AlertTitle>
            <AlertDescription>
              <MergeList
                items={preview.blockers}
                rowKey={(item) => JSON.stringify(item)}
              >
                {(item) => (
                  <div className="flex flex-col gap-1">
                    <p>{blockers[item.kind]}</p>
                    {(item.namespace || item.media_uuid) && (
                      <code data-selectable-text className="wrap-anywhere">
                        {item.namespace
                          ? `${item.namespace} · ${item.value}`
                          : item.media_uuid}
                      </code>
                    )}
                  </div>
                )}
              </MergeList>
            </AlertDescription>
          </Alert>
        )}
        {preview.selection && (
          <p>
            {labels.selection[preview.selection.mode]} ·{" "}
            {intl.formatMessage(
              {
                id: "post_merge.positions",
                defaultMessage:
                  "{count, plural, one {# source position} other {# source positions}}",
              },
              { count: preview.selection.entry_count },
            )}
          </p>
        )}
        {preview.album && (
          <>
            <p>{labels.action[preview.album.action]}</p>
            {preview.album.title && (
              <p data-selectable-text className="wrap-anywhere">
                {preview.album.title}
              </p>
            )}
            <p>
              {intl.formatMessage(
                {
                  id: "post_merge.membership",
                  defaultMessage:
                    "{added, number} gallery members added; {removed, number} removed.",
                },
                {
                  added: preview.album.add.length,
                  removed: preview.album.remove.length,
                },
              )}
            </p>
            <PostSection
              title={msg(
                "post_merge.order_preview",
                "Resulting source order and gallery changes",
              )}
            >
              <MergeList
                items={preview.album.entries}
                rowKey={(item) => String(item.position)}
              >
                {(entry) => (
                  <div className="flex flex-col gap-1">
                    <p data-selectable-text className="wrap-anywhere">
                      {entry.position + 1} · {entry.namespace} · {entry.value}
                    </p>
                    <p>{statuses[entry.status]}</p>
                  </div>
                )}
              </MergeList>
              {preview.album.add.length > 0 && (
                <>
                  <p>{msg("post_merge.add_members", "Add to gallery")}</p>
                  <MergeList
                    items={preview.album.add}
                    rowKey={(item) => item.uuid}
                  >
                    {(item) => <MergeEntity item={item} />}
                  </MergeList>
                </>
              )}
              {preview.album.remove.length > 0 && (
                <>
                  <p>
                    {msg("post_merge.remove_members", "Remove from gallery")}
                  </p>
                  <MergeList
                    items={preview.album.remove}
                    rowKey={(item) => item.uuid}
                  >
                    {(item) => <MergeEntity item={item} />}
                  </MergeList>
                </>
              )}
            </PostSection>
          </>
        )}
        <PostSection
          title={msg(
            "post_merge.result_links",
            "Resulting post and attachment links",
          )}
        >
          <MergeList items={preview.media} rowKey={(item) => item.media_uuid}>
            {(item) => (
              <p data-selectable-text className="wrap-anywhere">
                {labels.state[item.state]} · {item.media_uuid}
              </p>
            )}
          </MergeList>
          <MergeList items={preview.attachments} rowKey={attachmentKey}>
            {(item) => (
              <p data-selectable-text className="wrap-anywhere">
                {item.namespace} · {item.value} · {labels.state[item.state]}
                {item.media_uuid && ` · ${item.media_uuid}`}
              </p>
            )}
          </MergeList>
        </PostSection>
      </CardContent>
    </Card>
  );
}

export function MergeReceipt({ receipt }: { receipt: PostMergeReceipt }) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useMergeLabels();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{msg("post_merge.saved", "Post merge saved")}</CardTitle>
        <CardDescription>
          {msg(
            "post_merge.saved_help",
            "Both posts now resolve to the selected post. Original evidence and earlier choices remain in the archive.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p>
          {intl.formatDate(receipt.result.consolidation.created_at, {
            dateStyle: "medium",
            timeStyle: "short",
          })}
        </p>
        {receipt.request.reason && (
          <p data-selectable-text className="wrap-anywhere">
            {receipt.request.reason}
          </p>
        )}
        <MergePostLink id={receipt.request.destination_uuid}>
          {msg("post_merge.open_result", "Open resulting post")}
        </MergePostLink>
        <p>{labels.action[receipt.result.gallery.action]}</p>
        <p>
          {intl.formatMessage(
            {
              id: "post_merge.membership",
              defaultMessage:
                "{added, number} gallery members added; {removed, number} removed.",
            },
            {
              added: receipt.result.gallery.added.length,
              removed: receipt.result.gallery.removed.length,
            },
          )}
        </p>
      </CardContent>
    </Card>
  );
}
