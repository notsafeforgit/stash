import { isValidElement, type ReactNode } from "react";
import {
  createToaster,
  type Toaster,
  type ToastOptions as ToasterOptions,
} from "@ui-sdk/react/toast";
import { errorToString } from "@/utils/errors";

export interface ToastOptions {
  /** A toast with this id that is still showing is updated instead of added. */
  id?: string;
  description?: ReactNode;
  duration?: number;
  action?: { label: ReactNode; onClick: () => void };
  /** Shows the toast in the region with this id, like the lightbox's. */
  toasterId?: string;
}

type ToastKind =
  | "default"
  | "success"
  | "info"
  | "warning"
  | "error"
  | "loading";

const DEFAULT_DURATION = 4000;

const defaultToaster = createToaster<ReactNode>({
  duration: DEFAULT_DURATION,
});
const toasters = new Map<string, Toaster<ReactNode>>();

/** The queue behind a region. Without an id, it is the app-wide queue. */
export function getToaster(id?: string): Toaster<ReactNode> {
  if (!id) return defaultToaster;
  let toaster = toasters.get(id);
  if (!toaster) {
    toaster = createToaster<ReactNode>({ duration: DEFAULT_DURATION });
    toasters.set(id, toaster);
  }
  return toaster;
}

function toSdkOptions(options: ToastOptions = {}): ToasterOptions<ReactNode> {
  const { action, toasterId: _toasterId, ...rest } = options;
  return {
    ...rest,
    action: action && { label: action.label, onAction: action.onClick },
  };
}

function show(
  kind: ToastKind,
  message: ReactNode,
  options?: ToastOptions,
): string {
  const toaster = getToaster(options?.toasterId);
  const sdkOptions = toSdkOptions(options);
  return kind === "default"
    ? toaster(message, sdkOptions)
    : toaster[kind](message, sdkOptions);
}

function errorMessage(error: unknown): ReactNode {
  if (typeof error === "string" || isValidElement(error)) return error;
  return errorToString(error);
}

/** Sonner-shaped API over ui-sdk toasts. Works outside React components. */
export const toast = Object.assign(
  (message: ReactNode, options?: ToastOptions) =>
    show("default", message, options),
  {
    message: (message: ReactNode, options?: ToastOptions) =>
      show("default", message, options),
    success: (message: ReactNode, options?: ToastOptions) =>
      show("success", message, options),
    info: (message: ReactNode, options?: ToastOptions) =>
      show("info", message, options),
    warning: (message: ReactNode, options?: ToastOptions) =>
      show("warning", message, options),
    loading: (message: ReactNode, options?: ToastOptions) =>
      show("loading", message, options),
    /** Anything that isn't text or an element is shown as its error message. */
    error: (error: unknown, options?: ToastOptions) =>
      show("error", errorMessage(error), options),
    promise<T>(
      promise: Promise<T>,
      messages: {
        loading: ReactNode;
        success: ReactNode | ((value: T) => ReactNode);
        error?: ReactNode | ((error: unknown) => ReactNode);
      },
      options?: ToastOptions,
    ): string {
      return getToaster(options?.toasterId).promise(
        promise,
        messages,
        toSdkOptions(options),
      );
    },
    /** Closes one toast by id, or every toast without one. */
    dismiss(id?: string) {
      for (const toaster of [defaultToaster, ...toasters.values()]) {
        toaster.dismiss(id);
      }
    },
  },
);
