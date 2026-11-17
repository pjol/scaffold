import { errorMessage, useCrmMailAccounts, useCrmRenderMail, useCrmSendMail, useCrmTemplates, type CrmContact, type CrmEmailTemplate } from "@scaffoldapp/shared";
import { Button, Chip, Field, Input, Sheet, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { useEffect, useState } from "react";
import { ScrollView, View } from "react-native";

/** Email a contact from your connected mailbox; the sent email lands on the timeline. */
export function ComposeSheet({ orgId, contact, open, onClose }: { orgId: string; contact: CrmContact; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const accounts = useCrmMailAccounts(orgId);
  const templates = useCrmTemplates(orgId);
  const render = useCrmRenderMail();
  const send = useCrmSendMail(contact.id);
  const senders = (accounts.data?.accounts ?? []).filter((a) => a.can_send);
  const [fromId, setFromId] = useState("");
  const [to, setTo] = useState(contact.emails[0] ?? "");
  const [cc, setCc] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!fromId && senders[0]) setFromId(senders[0].connection_id);
  }, [senders, fromId]);

  const useTemplate = (t: CrmEmailTemplate) =>
    render.mutate(
      { contact_id: contact.id, subject: t.subject, body: t.body },
      {
        onSuccess: (r) => {
          setSubject(r.subject);
          setBody(r.body);
          setError(r.missing.length ? `Fill in by hand: ${r.missing.join(", ")}` : null);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't fill in the template.")),
      },
    );
  const submit = () => {
    if (!fromId) return setError("Connect Gmail or Outlook in Settings → Connections first.");
    if (!subject.trim()) return setError("Add a subject.");
    if (!body.trim()) return setError("Write the message first.");
    setError(null);
    send.mutate(
      { connection_id: fromId, to: [to.trim()], cc: cc.split(/[\s,;]+/).filter(Boolean), subject: subject.trim(), body },
      {
        onSuccess: () => {
          toast.success("Sent and logged");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't send it.")),
      },
    );
  };
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={`Email ${contact.name}`}
      snapPoints={["90%"]}
      footer={
        <Button fullWidth loading={send.isPending} onPress={submit}>
          Send
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        {senders.length > 1 ? (
          <Field label="From">
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }}>
              {senders.map((a) => (
                <Chip key={a.connection_id} size="sm" selected={fromId === a.connection_id} onPress={() => setFromId(a.connection_id)}>
                  {a.account}
                </Chip>
              ))}
            </ScrollView>
          </Field>
        ) : senders[0] ? (
          <Text role="caption" tone="muted">
            From {senders[0].account}
          </Text>
        ) : null}
        <Field label="To">
          <Input value={to} onChangeText={setTo} autoCapitalize="none" keyboardType="email-address" />
        </Field>
        <Field label="Cc">
          <Input value={cc} onChangeText={setCc} autoCapitalize="none" keyboardType="email-address" placeholder="Separate with commas" />
        </Field>
        {(templates.data?.items.length ?? 0) > 0 ? (
          <Field label="Template">
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: theme.space[2] }}>
              {(templates.data?.items ?? []).map((t) => (
                <Chip key={t.id} size="sm" onPress={() => useTemplate(t)}>
                  {t.name}
                </Chip>
              ))}
            </ScrollView>
          </Field>
        ) : null}
        <Field label="Subject" required>
          <Input value={subject} onChangeText={setSubject} maxLength={200} />
        </Field>
        <Field label="Message" error={error}>
          <TextArea value={body} onChangeText={setBody} maxLength={20000} />
        </Field>
      </View>
    </Sheet>
  );
}
