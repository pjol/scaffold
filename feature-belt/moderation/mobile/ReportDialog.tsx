import { errorMessage, REPORT_REASONS, useBlockUser, useCreateReport, useIsBlocked, useMe, type ReportReason } from "@scaffoldapp/shared";
import { Button, Checkbox, Field, haptic, IconButton, Sheet, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { Check, Flag } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";

const NOTE_MAX = 1000;

/** The person behind reported content; adds an "Also block <name>" option. */
export interface ReportAuthor {
  id: string;
  name: string;
}

export interface ReportDialogProps {
  open: boolean;
  onClose: () => void;
  /** "user" for a person, or a registered content kind: "chat.message", "feed.post", "feed.comment", "assistant.message". */
  targetKind: string;
  targetId: string;
  /** For the title: "message" → "Report this message". Defaults to "person" for users, else "content". */
  noun?: string;
  /** Offer to block the author (or the reported person) in the same step. */
  author?: ReportAuthor | null;
  /** After the sheet finished closing (start the next modal from here). */
  onDismissed?: () => void;
  onReported?: (result: { blocked: boolean }) => void;
}

function ReasonRow({ label, selected, onPress }: { label: string; selected: boolean; onPress: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ checked: selected }}
      accessibilityLabel={label}
      onPress={onPress}
      style={{
        flexDirection: "row",
        alignItems: "center",
        gap: s[3],
        minHeight: theme.tap.base,
        paddingVertical: s[2],
        paddingHorizontal: s[3],
        borderRadius: theme.radius.md,
        borderWidth: 1,
        borderColor: selected ? c.primary : c.border,
        backgroundColor: selected ? c.primarySoft : "transparent",
      }}
    >
      <View
        style={{
          width: s[5],
          height: s[5],
          borderRadius: s[5],
          borderWidth: 1.5,
          borderColor: selected ? c.primary : c.borderStrong,
          backgroundColor: selected ? c.primary : "transparent",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        {selected ? <Check size={12} color={c.primaryFg} strokeWidth={3} /> : null}
      </View>
      <Text style={{ flexShrink: 1 }}>{label}</Text>
    </Pressable>
  );
}

/**
 * Report a person or content, as a bottom sheet. Controlled: open it from your own action
 * sheet or button. Sends `POST /v1/moderation/reports`; with `author`, can block them too.
 *
 * ```tsx
 * <ReportDialog open={open} onClose={() => setOpen(false)} targetKind="chat.message" targetId={m.id} noun="message"
 *   author={{ id: m.sender.id, name: m.sender.display_name }} />
 * ```
 */
export function ReportDialog({ open, onClose, targetKind, targetId, noun, author, onDismissed, onReported }: ReportDialogProps) {
  const theme = useTheme();
  const s = theme.space;
  const me = useMe();
  const report = useCreateReport();
  const block = useBlockUser();
  const alreadyBlocked = useIsBlocked(author?.id);
  const [reason, setReason] = useState<ReportReason | null>(null);
  const [note, setNote] = useState("");
  const [alsoBlock, setAlsoBlock] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setReason(null);
    setNote("");
    setAlsoBlock(false);
    setError(null);
  }, [open]);

  const label = noun ?? (targetKind === "user" ? "person" : "content");
  const canBlock = !!author && author.id !== me.data?.id && !alreadyBlocked;
  const busy = report.isPending || block.isPending;

  const submit = async () => {
    if (!reason) {
      setError("Choose a reason.");
      return;
    }
    try {
      await report.mutateAsync({ target_kind: targetKind, target_id: targetId, reason, note: note.trim() });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't send the report. Try again."));
      return;
    }
    let blocked = false;
    if (alsoBlock && author) {
      try {
        await block.mutateAsync(author.id);
        blocked = true;
      } catch (e) {
        toast.error(errorMessage(e, `Report sent, but couldn't block ${author.name}. Try again.`));
      }
    }
    haptic.success();
    toast.success(blocked && author ? `Report sent and ${author.name} blocked` : "Report sent. Thank you!");
    onClose();
    onReported?.({ blocked });
  };

  return (
    <Sheet
      open={open}
      onClose={() => !busy && onClose()}
      {...(onDismissed ? { onDismissed } : {})}
      title={`Report this ${label}`}
      info="Reports go to our moderators, who review them within 24 hours. The person you report isn't told who reported them."
      snapPoints={["92%"]}
      footer={
        <Button fullWidth variant="danger" loading={busy} onPress={() => void submit()}>
          Send report
        </Button>
      }
    >
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ gap: s[6], paddingBottom: s[6] }} showsVerticalScrollIndicator={false}>
        <Field label="Reason" error={error}>
          <View accessibilityRole="radiogroup" style={{ gap: s[2] }}>
            {REPORT_REASONS.map((r) => (
              <ReasonRow
                key={r.value}
                label={r.label}
                selected={reason === r.value}
                onPress={() => {
                  haptic.selection();
                  setReason(r.value);
                  setError(null);
                }}
              />
            ))}
          </View>
        </Field>
        <Field label="Details" info="Optional. Only moderators see this.">
          <TextArea value={note} onChangeText={(t) => setNote(t.slice(0, NOTE_MAX))} maxLength={NOTE_MAX} />
        </Field>
        {canBlock && author ? (
          <Checkbox checked={alsoBlock} onCheckedChange={setAlsoBlock} label={`Also block ${author.name}`} info="You won't see each other's content or be able to message each other." />
        ) : null}
      </ScrollView>
    </Sheet>
  );
}

export interface ReportButtonProps extends Omit<ReportDialogProps, "open" | "onClose"> {}

/** A flag icon button that opens {@link ReportDialog}. */
export function ReportButton(props: ReportButtonProps) {
  const [open, setOpen] = useState(false);
  const label = `Report this ${props.noun ?? (props.targetKind === "user" ? "person" : "content")}`;
  return (
    <>
      <IconButton label={label} icon={Flag} variant="ghost" onPress={() => setOpen(true)} />
      <ReportDialog {...props} open={open} onClose={() => setOpen(false)} />
    </>
  );
}
