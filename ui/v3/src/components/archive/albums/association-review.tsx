import { useMemo } from "react";
import {
  createGalleryAssociationAPI,
  createAttachmentMediaAPI,
  type GalleryAssociationAPI,
  type GalleryAssociationPreview,
  type AttachmentMediaAPI,
  type AttachmentMediaPreview,
  type AttachmentMediaContext,
} from "@/core/native-archive/association-review-api";
import {
  AssociationEditor,
  type AssociationEditorFormProps,
} from "./association-editor";
import { AssociationForm } from "./association-form";
import { AssociationHistory } from "./association-details";

function GalleryForm({
  generation,
  ...props
}: AssociationEditorFormProps<
  Awaited<ReturnType<GalleryAssociationAPI["context"]>>,
  GalleryAssociationPreview,
  Pick<GalleryAssociationAPI, "endpoint" | "preview">
>) {
  return (
    <AssociationForm
      key={`${props.current.post.uuid}:${generation}`}
      family="gallery"
      {...props}
    />
  );
}
function AttachmentForm({
  generation,
  ...props
}: AssociationEditorFormProps<
  AttachmentMediaContext,
  AttachmentMediaPreview,
  Pick<AttachmentMediaAPI, "endpoint" | "preview">
>) {
  return (
    <AssociationForm
      key={`${props.current.attachment.uuid}:${generation}`}
      family="attachment"
      {...props}
    />
  );
}

export function GalleryAssociationReview({
  post,
  endpoint,
  onChanged,
}: {
  post: string;
  endpoint: string;
  onChanged: () => void | Promise<void>;
}) {
  const api = useMemo(() => createGalleryAssociationAPI(endpoint), [endpoint]);
  return (
    <AssociationEditor
      api={api}
      scope={post}
      onChanged={onChanged}
      history={<AssociationHistory family="gallery" api={api} scope={post} />}
      Form={GalleryForm}
    />
  );
}
export function AttachmentMediaReview({
  attachment,
  endpoint,
  onChanged,
}: {
  attachment: string;
  endpoint: string;
  onChanged: () => void | Promise<void>;
}) {
  const api = useMemo(() => createAttachmentMediaAPI(endpoint), [endpoint]);
  return (
    <AssociationEditor
      api={api}
      scope={attachment}
      onChanged={onChanged}
      history={
        <AssociationHistory family="attachment" api={api} scope={attachment} />
      }
      Form={AttachmentForm}
    />
  );
}
