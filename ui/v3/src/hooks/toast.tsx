import { toast, type ExternalToast } from "sonner";
import { errorToString } from "src/utils/errors";

const Toast = {
  success(message: string, options?: ExternalToast) {
    return toast.success(message, options);
  },
  error(error: unknown, options?: ExternalToast) {
    return toast.error(errorToString(error), options);
  },
  /**
   * Show a single toast that progresses through loading → success/error
   * as the supplied promise resolves. Use for long-running synchronous
   * server actions (backup, anonymise, etc.) where the user otherwise
   * sees nothing happen for tens of seconds.
   *
   * `success` and `error` may be either a static string or a function
   * that derives the toast text from the resolved value / thrown error.
   */
  promise<T>(
    promise: Promise<T>,
    opts: {
      loading: string;
      success: string | ((value: T) => string);
      error?: string | ((error: unknown) => string);
    },
  ) {
    toast.promise(promise, {
      loading: opts.loading,
      success: (value: T) =>
        typeof opts.success === "function" ? opts.success(value) : opts.success,
      error: (e: unknown) => {
        if (typeof opts.error === "function") return opts.error(e);
        if (typeof opts.error === "string") return opts.error;
        return errorToString(e);
      },
    });
    return promise;
  },
};

export function useToast() {
  return Toast;
}
