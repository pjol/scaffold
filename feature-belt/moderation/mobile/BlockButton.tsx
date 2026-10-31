import { errorMessage, useBlockUser, useIsBlocked, useMe, useUnblockUser } from "@scaffoldapp/shared";
import { Button, ConfirmDialog, haptic, Text, toast, type ButtonProps } from "@scaffoldapp/ui-native";
import { Ban, Undo2 } from "lucide-react-native";
import { useState } from "react";

export interface BlockDialogProps {
  open: boolean;
  onClose: () => void;
  userId: string;
  /** Their display name, for the title and toast. */
  name: string;
  onBlocked?: () => void;
}

/**
 * Confirm-and-block. Controlled: open it from your own action sheet ("Block Jordan").
 * Blocking hides both people's content from each other and stops messages both ways.
 */
export function BlockDialog({ open, onClose, userId, name, onBlocked }: BlockDialogProps) {
  const block = useBlockUser();
  return (
    <ConfirmDialog
      open={open}
      onClose={onClose}
      title={`Block ${name}?`}
      tone="danger"
      confirmLabel="Block"
      errorText={(e) => errorMessage(e, "Couldn't block them. Try again.")}
      onConfirm={async () => {
        await block.mutateAsync(userId);
        haptic.success();
        toast.success(`${name} blocked`);
        onBlocked?.();
      }}
    >
      <Text tone="muted">You won't see each other's posts or messages, and neither of you can message the other. They aren't told. Unblock any time in Settings → Safety.</Text>
    </ConfirmDialog>
  );
}

export interface BlockButtonProps {
  userId: string;
  name: string;
  variant?: Extract<ButtonProps["variant"], "secondary" | "outline" | "ghost">;
  fullWidth?: boolean;
  onBlocked?: () => void;
  onUnblocked?: () => void;
}

/** "Block" (with confirmation) or "Unblock" for one person. Renders nothing for the caller's own id. */
export function BlockButton({ userId, name, variant = "secondary", fullWidth, onBlocked, onUnblocked }: BlockButtonProps) {
  const me = useMe();
  const blocked = useIsBlocked(userId);
  const unblock = useUnblockUser();
  const [confirm, setConfirm] = useState(false);
  if (me.data?.id === userId) return null;
  if (blocked) {
    return (
      <Button
        variant={variant}
        leftIcon={Undo2}
        fullWidth={fullWidth}
        loading={unblock.isPending}
        onPress={() =>
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
      <Button variant={variant} leftIcon={Ban} fullWidth={fullWidth} onPress={() => setConfirm(true)}>
        Block
      </Button>
      <BlockDialog open={confirm} onClose={() => setConfirm(false)} userId={userId} name={name} onBlocked={onBlocked} />
    </>
  );
}
