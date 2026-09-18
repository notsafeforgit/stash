import { fileNameFromPath } from "./file";

export interface ImageFileSource {
  src: string;
  filePath?: string;
}

export function canCopyImage(): boolean {
  return (
    window.isSecureContext &&
    typeof navigator.clipboard?.write === "function" &&
    typeof ClipboardItem === "function"
  );
}

async function fetchImage(src: string): Promise<Blob> {
  const response = await fetch(src);
  if (!response.ok) throw new Error("Image request failed");
  const blob = await response.blob();
  // An expired proxy login may redirect to an HTML page with status 200.
  if (!blob.type.startsWith("image/")) throw new Error("Not an image");
  return blob;
}

async function clipboardPNG(src: string): Promise<Blob> {
  const blob = await fetchImage(src);
  if (blob.type === "image/png") return blob;

  const url = URL.createObjectURL(blob);
  const image = new Image();
  const canvas = document.createElement("canvas");
  try {
    image.src = url;
    await image.decode();
    canvas.width = image.naturalWidth;
    canvas.height = image.naturalHeight;
    const context = canvas.getContext("2d");
    if (!context) throw new Error("Image conversion unavailable");
    context.drawImage(image, 0, 0);
    return await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((png) => {
        if (png) resolve(png);
        else reject(new Error("Image conversion failed"));
      }, "image/png");
    });
  } finally {
    URL.revokeObjectURL(url);
    image.src = "";
    canvas.width = canvas.height = 0;
  }
}

/** Call directly from the user's click. Safari must receive write() before
 * any await; ClipboardItem owns the asynchronous fetch/PNG conversion. */
export async function copyImage(source: ImageFileSource): Promise<void> {
  if (!canCopyImage()) throw new Error("Clipboard unavailable");
  const png = clipboardPNG(source.src);
  // A denied clipboard request may never consume its item's promise.
  void png.catch(() => {});
  return navigator.clipboard.write([new ClipboardItem({ "image/png": png })]);
}

function downloadName(source: ImageFileSource, mime: string): string {
  if (source.filePath) return fileNameFromPath(source.filePath);
  const extension = mime === "image/jpeg" ? "jpg" : mime.slice(6).split("+")[0];
  return `image.${extension || "png"}`;
}

/** Downloads the original bytes, retaining animation, HDR and metadata.
 * A blob URL also works when the configured backend is a different origin. */
export async function saveImage(source: ImageFileSource): Promise<void> {
  const blob = await fetchImage(source.src);
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  try {
    link.href = url;
    link.download = downloadName(source, blob.type);
    document.body.append(link);
    link.click();
  } finally {
    link.remove();
    // Give Safari time to consume the download before releasing its bytes.
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
  }
}
