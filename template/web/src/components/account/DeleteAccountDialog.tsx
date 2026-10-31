"use client";

import { errorMessage, useDeleteAccount } from "@scaffoldapp/shared";
import { ConfirmDialog } from "@scaffoldapp/ui-web";
import type { ReactNode } from "react";

export interface DeleteAccountDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Runs after the account is deleted (sign out and navigate). */
  onDeleted: () => Promise<void> | void;
  trigger?: ReactNode;
}

/**
 * Account deletion confirmation (Apple 5.1.1(v), Play): states what is deleted now, what
 * is kept and why, and requires typing DELETE. A failure keeps the dialog open with the
 * error. Features that keep records (payments) add their line here when installed.
 */
export function DeleteAccountDialog({ open, onOpenChange, onDeleted, trigger }: DeleteAccountDialogProps) {
  const del = useDeleteAccount();
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      trigger={trigger}
      title="Delete your account?"
      tone="danger"
      confirmLabel="Delete account"
      requireText="DELETE"
      errorText={(e) => errorMessage(e, "Couldn't delete your account. Try again.")}
      onConfirm={async () => {
        await del.mutateAsync();
        await onDeleted();
      }}
    >
      <ul className="flex list-disc flex-col gap-2 pl-5 marker:text-fg-muted">
        <li>
          <span className="font-semibold">Deleted now:</span> your name, email, photo and uploaded files. You can't sign in to this account again.
        </li>
        <li>
          <span className="font-semibold">Kept without your name or email:</span> when you accepted the Terms and Privacy Policy, as the record of consent the law requires.
        </li>
        <li>It can't be undone.</li>
      </ul>
    </ConfirmDialog>
  );
}
