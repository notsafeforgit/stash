import type React from "react";
import { useCallback, useEffect } from "react";

const blobToDataURL = (blob: Blob): Promise<string> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => resolve(reader.result as string);
    reader.onerror = reject;
    reader.readAsDataURL(blob);
  });

const entityImageAccept =
  "image/jpeg,image/png,image/webp,image/gif,image/avif";

const heicBrands = new Set([
  "heic",
  "heix",
  "hevc",
  "hevx",
  "heim",
  "heis",
  "hevm",
  "hevs",
]);

async function isHEICBlob(blob: Blob, fileName?: string): Promise<boolean> {
  const type = blob.type.toLowerCase();
  if (type === "image/heic" || type === "image/heif") return true;
  if (fileName && /\.(heic|heif)$/i.test(fileName)) return true;

  const header = new Uint8Array(await blob.slice(0, 64).arrayBuffer());
  if (header.length < 12) return false;
  const brandHeader = String.fromCharCode(...header.slice(4, 8));
  if (brandHeader !== "ftyp") return false;

  for (let i = 8; i + 4 <= header.length; i += 4) {
    const brand = String.fromCharCode(...header.slice(i, i + 4));
    if (heicBrands.has(brand)) return true;
  }

  return false;
}

async function entityImageBlobToDataURL(
  blob: Blob,
  fileName?: string,
): Promise<string> {
  if (await isHEICBlob(blob, fileName)) {
    throw new Error(
      "HEIC/HEIF images are not supported for entity images. Use JPEG, PNG, WebP, GIF, or AVIF.",
    );
  }

  return blobToDataURL(blob);
}

const readImage = async (
  file: File,
  onLoadEnd: (imageData: string) => void,
) => {
  onLoadEnd(await entityImageBlobToDataURL(file, file.name));
};

const onImageChange = (
  event: React.FormEvent<HTMLInputElement>,
  onLoadEnd: (imageData: string) => void,
) => {
  const file = event?.currentTarget?.files?.[0];
  if (file) return readImage(file, onLoadEnd);
  return Promise.resolve();
};

const imageToDataURL = async (url: string) => {
  const response = await fetch(url);
  const blob = await response.blob();
  return entityImageBlobToDataURL(blob, url);
};

// uses event.clipboardData which works in all contexts including insecure HTTP
const pasteImage = (
  event: ClipboardEvent,
  onLoadEnd: (imageData: string) => void,
) => {
  const files = event?.clipboardData?.files;
  if (!files?.length) return;

  if (document.activeElement instanceof HTMLInputElement) {
    // don't interfere with pasting text into inputs
    return;
  }

  const file = Array.from(files).find((f) => f.type.startsWith("image/"));
  if (file) void readImage(file, onLoadEnd).catch(() => {});
};

// uses Clipboard API which requires secure context (HTTPS or localhost)
const readClipboardImage = async (): Promise<string | null> => {
  if (!window.isSecureContext) {
    return null;
  }

  const items = await navigator.clipboard.read();
  for (const item of items) {
    const imageType = item.types.find((t) => t.startsWith("image/"));
    if (imageType) {
      const blob = await item.getType(imageType);
      return entityImageBlobToDataURL(blob);
    }
  }
  return null;
};

const usePasteImage = (
  onLoadEnd: (imageData: string) => void,
  isActive: boolean = true,
) => {
  const encodeImage = useCallback(
    (data: string) => {
      onLoadEnd(data);
    },
    [onLoadEnd],
  );

  useEffect(() => {
    const paste = (event: ClipboardEvent) => pasteImage(event, encodeImage);
    if (isActive) {
      document.addEventListener("paste", paste);
    }

    return () => document.removeEventListener("paste", paste);
  }, [isActive, encodeImage]);

  return false;
};

const ImageUtils = {
  entityImageAccept,
  onImageChange,
  usePasteImage,
  imageToDataURL,
  readClipboardImage,
};

export default ImageUtils;
