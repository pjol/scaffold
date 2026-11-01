import {
  CRM_ACTIVITY_LABEL,
  CRM_CALL_OUTCOMES,
  CRM_MEETING_OUTCOMES,
  errorMessage,
  useCrmLogActivity,
  type CrmActivityRequest,
  type CrmObjectType,
  type CrmRecordRef,
} from "@scaffoldapp/shared";
import { Button, Chip, Field, Input, SegmentedControl, Sheet, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";

type Kind = "note" | "call" | "email" | "meeting" | "task";

const DUE: { id: string; label: string; days: number }[] = [
  { id: "today", label: "Today", days: 0 },
  { id: "tomorrow", label: "Tomorrow", days: 1 },
  { id: "3d", label: "In 3 days", days: 3 },
  { id: "week", label: "In a week", days: 7 },
];

/** Log a note, call, email or meeting, or add a task, on a record (also on the suggested related records). */
export function LogSheet({
  type,
  id,
  alsoOn,
  open,
  onClose,
  initialKind = "note",
}: {
  type: CrmObjectType;
  id: string;
  alsoOn: CrmRecordRef[];
  open: boolean;
  onClose: () => void;
  initialKind?: Kind;
}) {
  const theme = useTheme();
  const log = useCrmLogActivity(type, id);
  const [kind, setKind] = useState<Kind>(initialKind);
  const [body, setBody] = useState("");
  const [subject, setSubject] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);
  const [due, setDue] = useState("tomorrow");
  const [error, setError] = useState<string | null>(null);

  const submit = () => {
    let req: CrmActivityRequest;
    if (kind === "task") {
      if (!subject.trim()) return setError("Give the task a title.");
      const d = new Date();
      d.setDate(d.getDate() + (DUE.find((x) => x.id === due)?.days ?? 1));
      d.setHours(9, 0, 0, 0);
      req = { type: "task", subject: subject.trim(), due_at: d.toISOString(), reminder_at: new Date(d.getTime() - 15 * 60_000).toISOString(), ...(body.trim() ? { body: body.trim() } : {}) };
    } else {
      if (kind === "note" && !body.trim()) return setError("Write the note first.");
      if (kind !== "note" && !body.trim() && !subject.trim() && !outcome) return setError("Add what happened before logging it.");
      req = {
        type: kind,
        body: body.trim(),
        ...(subject.trim() ? { subject: subject.trim() } : {}),
        ...(outcome ? { outcome } : {}),
        ...(kind === "call" || kind === "email" ? { direction: "outbound" as const } : {}),
      };
    }
    req.also_on = alsoOn.map((r) => ({ type: r.type, id: r.id }));
    setError(null);
    log.mutate(req, {
      onSuccess: (a) => {
        toast.success(kind === "task" ? "Task added" : `${CRM_ACTIVITY_LABEL[kind]} logged`);
        if (a.unreachable?.length) toast.info(`${a.unreachable.join(", ")} can't see this record, so wasn't notified.`);
        setBody("");
        setSubject("");
        setOutcome(null);
        onClose();
      },
      onError: (e) => setError(errorMessage(e, "Couldn't save it.")),
    });
  };

  const outcomes = kind === "call" ? CRM_CALL_OUTCOMES : kind === "meeting" ? CRM_MEETING_OUTCOMES : [];
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title="Log activity"
      snapPoints={["85%"]}
      footer={
        <Button fullWidth loading={log.isPending} onPress={submit}>
          {kind === "task" ? "Add task" : kind === "note" ? "Save note" : `Log ${CRM_ACTIVITY_LABEL[kind].toLowerCase()}`}
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <SegmentedControl<Kind>
          accessibilityLabel="What to log"
          value={kind}
          onValueChange={(v) => {
            setKind(v);
            setOutcome(null);
          }}
          options={[
            { value: "note", label: "Note" },
            { value: "call", label: "Call" },
            { value: "meeting", label: "Meet" },
            { value: "task", label: "Task" },
          ]}
        />
        {kind === "task" || kind === "meeting" || kind === "email" ? (
          <Field label={kind === "task" ? "Title" : kind === "email" ? "Subject" : "Title"} required={kind === "task"}>
            <Input value={subject} onChangeText={setSubject} maxLength={200} />
          </Field>
        ) : null}
        {kind === "task" ? (
          <Field label="Due">
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }}>
              {DUE.map((d) => (
                <Chip key={d.id} size="sm" selected={due === d.id} onPress={() => setDue(d.id)}>
                  {d.label}
                </Chip>
              ))}
            </ScrollView>
          </Field>
        ) : null}
        {outcomes.length > 0 ? (
          <Field label="Outcome">
            <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
              {outcomes.map((o) => (
                <Chip key={o.value} size="sm" selected={outcome === o.value} onPress={() => setOutcome(outcome === o.value ? null : o.value)}>
                  {o.label}
                </Chip>
              ))}
            </View>
          </Field>
        ) : null}
        <Field label={kind === "task" ? "Notes" : kind === "note" ? "Note" : "What happened"} error={error}>
          <TextArea value={body} onChangeText={setBody} maxLength={20000} />
        </Field>
        {alsoOn.length > 0 ? (
          <Text role="caption" tone="muted">
            Also logged on {alsoOn.map((r) => r.name).join(", ")}
          </Text>
        ) : null}
      </View>
    </Sheet>
  );
}
