"use client";

import { errorMessage, useBlockUser, useIsBlocked, useMe, useUnblockUser } from "@scaffoldapp/shared";
import { Button, ConfirmDialog, toast, type ButtonProps } from "@scaffoldapp/ui-web";
import { Ban, Undo2 } from "lucide-react";
import { useState } from "react";

export interface BlockDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  userId: string;
  /** Their display name, for the title and toast. */
  name: string;
  onBlocked?: () => void;
}

/**
 * Confirm-and-block. Controlled: open it from your own menu item ("Block Jordan").
 * Blocking hides both people's content from each other and stops messages both ways.
 */
export function BlockDialog({ open, onOpenChange, userId, name, onBlocked }: BlockDialogProps) {
  const block = useBlockUser();
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Block ${name}?`}
      tone="danger"
      confirmLabel="Block"
      errorText={(e) => errorMessage(e, "Couldn't block them. Try again.")}
      onConfirm={async () => {
        await block.mutateAsync(userId);
        toast.success(`${name} blocked`);
        onBlocked?.();
      }}
    >
      <p>You won&apos;t see each other&apos;s posts or messages, and neither of you can message the other. They aren&apos;t told. Unblock any time in Settings → Safety.</p>
    </ConfirmDialog>
  );
}

export interface BlockButtonProps {
  userId: string;
  name: string;
  size?: ButtonProps["size"];
  variant?: Extract<ButtonProps["variant"], "secondary" | "outline" | "ghost">;
  className?: string;
  onBlocked?: () => void;
  onUnblocked?: () => void;
}

/**
 * "Block" (with confirmation) or "Unblock" for one person, from the caller's block list.
 * Renders nothing for the caller's own id.
 */
export function BlockButton({ userId, name, size = "auto", variant = "secondary", className, onBlocked, onUnblocked }: BlockButtonProps) {
  const me = useMe();
  const blocked = useIsBlocked(userId);
  const unblock = useUnblockUser();
  const [confirm, setConfirm] = useState(false);
  if (me.data?.id === userId) return null;
  if (blocked) {
    return (
      <Button
        variant={variant}
        size={size}
        leftIcon={<Undo2 />}
        className={className}
        loading={unblock.isPending}
        onClick={() =>
          unblock.mutate(userId, {
            onSuccess: () => {
              toast.success(`${name} unblocked`);
              onUnblocked?.();
            },
            onError: (e) => toast.error(errorMessage(e, "Couldn't unblock them. Try again.")),
          })
        }
      >
        Unblock
      </Button>
    );
  }
  return (
    <>
      <Button variant={variant} size={size} leftIcon={<Ban />} className={className} onClick={() => setConfirm(true)}>
        Block
      </Button>
      <BlockDialog open={confirm} onOpenChange={setConfirm} userId={userId} name={name} {...(onBlocked ? { onBlocked } : {})} />
    </>
  );
}
