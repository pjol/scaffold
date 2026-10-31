"use client";

import * as DialogPrimitive from "@radix-ui/react-dialog";
import { CircleAlert, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { focusFirstControl } from "../lib/focus";
import { useControllableState } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";
import { Button, type ButtonVariant } from "./Button";
import { IconButton } from "./IconButton";
import { InfoTip } from "./InfoTip";
import { Input } from "./Input";

export interface DialogProps {
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** Element that opens the dialog (rendered with `asChild`). */
  trigger?: ReactNode;
  /** Short title. No description: context goes in `info`. */
  title: ReactNode;
  /** ⓘ content next to the title. */
  info?: ReactNode;
  /** Body (a form, facts, a picker). */
  children?: ReactNode;
  /** Actions: right-aligned on desktop, stacked full width on phones. */
  footer?: ReactNode;
  /** Max width: `sm` 448 (confirm) · `md` 512 (default) · `lg` 672 (forms). */
  size?: "sm" | "md" | "lg";
  /** Show the close button. @default true */
  closable?: boolean;
  className?: string;
}

const widths = { sm: "max-w-md", md: "max-w-lg", lg: "max-w-2xl" } as const;

/**
 * Modal dialog: focus trap, focus restore and scroll lock (Radix), spring scale-in.
 * Opening focuses the first control in the body, then the footer (never the title's ⓘ or
 * Close). Sits at the bottom of the screen on phones. Padding 24 (20 narrow); title → body 16;
 * body → actions 24.
 */
export function Dialog({ open, defaultOpen = false, onOpenChange, trigger, title, info, children, footer, size = "md", closable = true, className }: DialogProps) {
  const t = useMotionTokens();
  const [isOpen, setOpen] = useControllableState({ value: open, defaultValue: defaultOpen, onChange: onOpenChange });
  const bodyRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLDivElement>(null);
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
            <div className="pointer-events-none fixed inset-0 z-(--app-z-modal) flex items-end justify-center p-3 pb-safe sm:items-center sm:p-6">
              <DialogPrimitive.Content asChild forceMount aria-describedby={undefined} onOpenAutoFocus={(e) => focusFirstControl(e, bodyRef.current, footerRef.current)}>
                <motion.div
                  className={cn(
                    "pointer-events-auto relative mb-3 flex max-h-[min(88dvh,50rem)] w-full flex-col overflow-hidden rounded-xl border border-border bg-surface-raised shadow-xl outline-none sm:mb-0",
                    widths[size],
                    className,
                  )}
                  initial={{ opacity: 0, scale: 0.96, y: 16 }}
                  animate={{ opacity: 1, scale: 1, y: 0 }}
                  exit={{ opacity: 0, scale: 0.98, y: 8, transition: { duration: t.duration.fast, ease: t.ease.in } }}
                  transition={{ ...t.spring.gentle, opacity: { duration: t.duration.fast } }}
                >
                  <div className="flex items-start gap-3 px-5 pt-5 sm:px-6 sm:pt-6">
                    <DialogPrimitive.Title className="flex min-w-0 flex-1 items-center gap-2 type-title-sm text-fg">
                      {title}
                      {info && <InfoTip content={info} size="md" />}
                    </DialogPrimitive.Title>
                    {closable && (
                      <DialogPrimitive.Close asChild>
                        <IconButton label="Close" icon={<X />} size="sm" variant="ghost" />
                      </DialogPrimitive.Close>
                    )}
                  </div>
                  {children !== undefined && children !== null ? (
                    <div ref={bodyRef} className="min-h-0 flex-1 overflow-y-auto px-5 pt-4 type-body text-fg sm:px-6">
                      {children}
                    </div>
                  ) : null}
                  {footer ? (
                    <div ref={footerRef} className="flex flex-col-reverse gap-2 px-5 pt-6 pb-5 sm:flex-row sm:justify-end sm:px-6 sm:pb-6">
                      {footer}
                    </div>
                  ) : (
                    <div className="pb-5 sm:pb-6" />
                  )}
                </motion.div>
              </DialogPrimitive.Content>
            </div>
          </DialogPrimitive.Portal>
        )}
      </AnimatePresence>
    </DialogPrimitive.Root>
  );
}

/** Closes the nearest Dialog or Sheet: `<DialogClose asChild><Button /></DialogClose>`. */
export const DialogClose = DialogPrimitive.Close;

export interface ConfirmDialogProps {
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  trigger?: ReactNode;
  /** Phrase it as the question, naming the object: "Delete this post?" */
  title: ReactNode;
  info?: ReactNode;
  /** Essential facts (what happens, what is kept). Not a description paragraph. */
  children?: ReactNode;
  /** Verb naming the outcome: "Delete account". @default "Confirm" */
  confirmLabel?: ReactNode;
  /** @default "Cancel" */
  cancelLabel?: ReactNode;
  /** `danger` for destructive actions. @default "primary" */
  tone?: Extract<ButtonVariant, "primary" | "danger" | "success">;
  /** Require typing this text (e.g. `DELETE`) before the confirm button enables. */
  requireText?: string;
  /**
   * May return a promise: the button shows a spinner and the dialog closes on resolve.
   * A throw keeps the dialog open and shows the error above the actions.
   */
  onConfirm: () => void | Promise<unknown>;
  /** Message for a failed `onConfirm`. @default the error's message */
  errorText?: ReactNode | ((error: unknown) => ReactNode);
  onError?: (error: unknown) => void;
}

function confirmErrorText(error: unknown, errorText: ConfirmDialogProps["errorText"]): ReactNode {
  if (typeof errorText === "function") return errorText(error);
  if (errorText !== undefined) return errorText;
  if (typeof error === "object" && error && "message" in error && typeof (error as { message: unknown }).message === "string" && (error as { message: string }).message) {
    return (error as { message: string }).message;
  }
  return "That didn't go through. Try again.";
}

/** Confirmation with an async-aware confirm button and optional type-to-confirm. */
export function ConfirmDialog({
  open,
  defaultOpen = false,
  onOpenChange,
  trigger,
  title,
  info,
  children,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  tone = "primary",
  requireText,
  onConfirm,
  errorText,
  onError,
}: ConfirmDialogProps) {
  const t = useMotionTokens();
  const inputId = useId();
  const [isOpen, setOpen] = useControllableState({ value: open, defaultValue: defaultOpen, onChange: onOpenChange });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ReactNode>(null);
  const [typed, setTyped] = useState("");
  useEffect(() => {
    if (isOpen) {
      setError(null);
      setTyped("");
    }
  }, [isOpen]);
  const ready = !requireText || typed.trim() === requireText;
  const confirm = async () => {
    if (!ready) return;
    setError(null);
    setBusy(true);
    try {
      await onConfirm();
    } catch (e) {
      setBusy(false);
      setError(confirmErrorText(e, errorText));
      onError?.(e);
      return;
    }
    setBusy(false);
    setOpen(false);
  };
  const hasBody = children !== undefined || requireText !== undefined || error !== null;
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(o) => !busy && setOpen(o)}
      trigger={trigger}
      title={title}
      info={info}
      size="sm"
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)} disabled={busy}>
            {cancelLabel}
          </Button>
          <Button variant={tone} size="auto" onClick={() => void confirm()} loading={busy} disabled={!ready}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {hasBody ? (
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void confirm();
          }}
        >
          {children}
          {requireText !== undefined && (
            <div className="flex flex-col gap-2">
              <label htmlFor={inputId} className="type-label text-fg">
                Type {requireText} to confirm
              </label>
              <Input id={inputId} value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" autoCapitalize="characters" spellCheck={false} disabled={busy} />
            </div>
          )}
          <AnimatePresence initial={false}>
            {error && (
              <motion.div
                key="error"
                role="alert"
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, transition: { duration: t.duration.fast } }}
                transition={t.spring.snappy}
                className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 type-label text-danger-strong"
              >
                <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                <span>{error}</span>
              </motion.div>
            )}
          </AnimatePresence>
        </form>
      ) : undefined}
    </Dialog>
  );
}
