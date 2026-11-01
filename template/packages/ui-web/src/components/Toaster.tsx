"use client";

import { CircleCheck, CircleX, Info, TriangleAlert } from "lucide-react";
import { Toaster as Sonner, toast, type ToasterProps as SonnerProps } from "sonner";
import { useColorMode } from "../theme/store";
import { Spinner } from "./Spinner";

export interface ToasterProps {
  /** @default "top-center" (clear of the bottom tab bar) */
  position?: SonnerProps["position"];
  /** ms. @default 4000 */
  duration?: number;
}

/**
 * Themed toasts (sonner). Mount once in the providers, then `toast.success("Saved")`,
 * `toast.error(errorMessage(e))`. One sentence; no description line.
 */
export function Toaster({ position = "top-center", duration = 4000 }: ToasterProps) {
  const { mode } = useColorMode();
  return (
    <Sonner
      theme={mode}
      position={position}
      duration={duration}
      offset={16}
      mobileOffset={12}
      gap={8}
      visibleToasts={3}
      icons={{
        success: <CircleCheck className="size-5" />,
        error: <CircleX className="size-5" />,
        warning: <TriangleAlert className="size-5" />,
        info: <Info className="size-5" />,
        loading: <Spinner size={18} />,
      }}
      toastOptions={{
        unstyled: true,
        classNames: {
          toast: "group pointer-events-auto flex w-full items-center gap-3 rounded-lg border border-border bg-surface-raised px-4 py-3 text-fg shadow-xl sm:w-96",
          title: "type-label font-semibold",
          description: "sr-only",
          icon: "flex shrink-0 items-center",
          success: "[&_[data-icon]]:text-success-strong",
          error: "[&_[data-icon]]:text-danger-strong",
          warning: "[&_[data-icon]]:text-warning-strong",
          info: "[&_[data-icon]]:text-info-strong",
          loading: "[&_[data-icon]]:text-primary-strong",
          actionButton: "ml-auto shrink-0 rounded-sm bg-primary px-3 py-2 type-label font-semibold text-primary-fg",
          cancelButton: "ml-auto shrink-0 rounded-sm bg-sunken px-3 py-2 type-label font-semibold text-fg",
          closeButton: "border-border bg-surface text-fg-muted",
        },
      }}
    />
  );
}

/** sonner's `toast`: `toast.success`, `toast.error`, `toast.promise`, `toast.dismiss`. */
export { toast };
