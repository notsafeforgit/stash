import {
  Toast,
  ToastAction,
  ToastDescription,
  ToastRegion,
  ToastTitle,
  type ToastType,
} from "@ui-sdk/react/toast";
import {
  CircleCheckIcon,
  InfoIcon,
  Loader2Icon,
  OctagonXIcon,
  TriangleAlertIcon,
  type LucideIcon,
} from "lucide-react";
import { getToaster } from "@/components/ui/toast";
import { useMediaQuery } from "@/utils/screen";

const icons: Partial<Record<ToastType, LucideIcon>> = {
  success: CircleCheckIcon,
  info: InfoIcon,
  warning: TriangleAlertIcon,
  error: OctagonXIcon,
  loading: Loader2Icon,
};

/** Renders the toasts sent to `id`, or the app-wide queue without one. */
const Toaster = ({ id }: { id?: string }) => {
  const isDesktop = useMediaQuery("(min-width: 768px)");

  return (
    <ToastRegion
      toaster={getToaster(id)}
      side={isDesktop ? "bottom" : "top"}
      align="end"
    >
      {(toast) => {
        const Icon = icons[toast.type];
        return (
          <Toast>
            {Icon && (
              <Icon
                aria-hidden="true"
                className={
                  toast.type === "loading" ? "animate-spin" : undefined
                }
              />
            )}
            <ToastTitle />
            <ToastDescription />
            <ToastAction data-variant="solid" />
          </Toast>
        );
      }}
    </ToastRegion>
  );
};

export { Toaster };
