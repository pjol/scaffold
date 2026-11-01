"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { AnimatePresence, motion, useDragControls, type PanInfo } from "motion/react";
import { useRef, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { focusFirstControl } from "../lib/focus";
import { useControllableState, useMediaQuery } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { IconButton } from "./IconButton";
import { InfoTip } from "./InfoTip";

export interface SheetProps {
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  trigger?: ReactNode;
  title: ReactNode;
  /** ⓘ next to the title. */
  info?: ReactNode;
  children?: ReactNode;
  /** Sticky footer: the screen's one primary action. */
  footer?: ReactNode;
  /** Desktop panel side. @default "right" */
  side?: "right" | "left";
  /** Desktop panel width: 384 · 448 · 640. @default "md" */
  size?: "sm" | "md" | "lg";
  /** Media query at which the sheet becomes a side panel. @default "(min-width: 768px)" */
  desktopQuery?: string;
  /** Drag down to dismiss on phones. @default true */
  dismissible?: boolean;
  className?: string;
}

const widths = { sm: "w-96", md: "w-md", lg: "w-160" } as const;

/**
 * Bottom sheet on phones (spring up; drag the handle or swipe down to dismiss) and a
 * side panel from 768 px. Focus goes to the first control (not the ⓘ or Close), except
 * a text field on touch, which would raise the keyboard mid-animation.
 */
export function Sheet({
  open,
  defaultOpen = false,
  onOpenChange,
  trigger,
  title,
  info,
  children,
  footer,
  side = "right",
  size = "md",
  desktopQuery = "(min-width: 768px)",
  dismissible = true,
  className,
}: SheetProps) {
  const t = useMotionTokens();
  const desktop = useMediaQuery(desktopQuery, false);
  const [isOpen, setOpen] = useControllableState({ value: open, defaultValue: defaultOpen, onChange: onOpenChange });
  const drag = useDragControls();
  const bodyRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLDivElement>(null);

  const onOpenAutoFocus = (e: Event) => focusFirstControl(e, bodyRef.current, footerRef.current, bodyRef.current);

  const onDragEnd = (_: unknown, info: PanInfo) => {
    if (info.offset.y > 120 || info.velocity.y > 700) setOpen(false);
  };

  const panelMotion = desktop
    ? { initial: { x: side === "right" ? "100%" : "-100%" }, animate: { x: 0 }, exit: { x: side === "right" ? "100%" : "-100%" } }
    : { initial: { y: "100%" }, animate: { y: 0 }, exit: { y: "100%" } };

  return (
    <DialogPrimitive.Root open={isOpen} onOpenChange={setOpen}>
      {trigger && <DialogPrimitive.Trigger asChild>{trigger}</DialogPrimitive.Trigger>}
      <AnimatePresence>
        {isOpen && (
          <DialogPrimitive.Portal forceMount>
            <DialogPrimitive.Overlay asChild forceMount>
              <motion.div
                className="fixed inset-0 z-(--app-z-overlay) bg-overlay"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: t.duration.base, ease: t.ease.out }}
              />
            </DialogPrimitive.Overlay>
            <DialogPrimitive.Content asChild forceMount aria-describedby={undefined} onOpenAutoFocus={onOpenAutoFocus}>
              <motion.div
                {...panelMotion}
                transition={t.spring.sheet}
                {...(!desktop && dismissible
                  ? { drag: "y" as const, dragListener: false, dragControls: drag, dragConstraints: { top: 0, bottom: 0 }, dragElastic: { top: 0.04, bottom: 0.9 }, onDragEnd }
                  : {})}
                className={cn(
                  "fixed z-(--app-z-modal) flex flex-col bg-surface-raised shadow-xl outline-none",
                  desktop
                    ? cn("inset-y-0 max-w-[calc(100vw-3rem)] border-border", widths[size], side === "right" ? "right-0 rounded-l-xl border-l" : "left-0 rounded-r-xl border-r")
                    : "inset-x-0 bottom-0 max-h-[92dvh] rounded-t-2xl border-t border-border pb-safe",
                  className,
                )}
              >
                {!desktop && (
                  <div className="flex h-6 shrink-0 cursor-grab touch-none items-center justify-center active:cursor-grabbing" onPointerDown={(e) => dismissible && drag.start(e)} aria-hidden>
                    <span className="h-1 w-8 rounded-pill bg-border-strong" />
                  </div>
                )}
                <div className={cn("flex items-center gap-3 px-4 sm:px-6", desktop ? "pt-6 pb-2" : "pb-2")} onPointerDown={(e) => !desktop && dismissible && drag.start(e)}>
                  <DialogPrimitive.Title className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
                    {title}
                    {info && <InfoTip content={info} size="md" />}
                  </DialogPrimitive.Title>
                  <DialogPrimitive.Close asChild>
                    <IconButton label="Close" icon={<X />} size="sm" variant="ghost" />
                  </DialogPrimitive.Close>
                </div>
                <div ref={bodyRef} tabIndex={-1} className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 outline-none sm:px-6">
                  {children}
                </div>
                {footer && (
                  <div ref={footerRef} className="shrink-0 border-t border-border px-4 pt-4 pb-4 sm:px-6">
                    {footer}
                  </div>
                )}
              </motion.div>
            </DialogPrimitive.Content>
          </DialogPrimitive.Portal>
        )}
      </AnimatePresence>
    </DialogPrimitive.Root>
  );
}
