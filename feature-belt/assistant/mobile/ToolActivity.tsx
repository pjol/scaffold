import {
  assistantSuggestionHref,
  assistantToolInputText,
  assistantToolLabel,
  errorMessage,
  useDecideAssistantToolCall,
  type AssistantActivity,
} from "@scaffoldapp/shared";
import { Badge, Button, Spinner, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Ban, CircleAlert, CircleCheck, ClipboardCheck, ShieldQuestion } from "lucide-react-native";
import { Platform, Pressable, ScrollView, View } from "react-native";

const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

/**
 * Tool calls behind a reply, one compact line each, with the connected service a call came
 * from. A change in a connected service that waits for the user shows its complete input
 * with Approve and Decline; a call the user's AI agent settings turned into a suggestion
 * links to its review.
 */
export function ToolActivity({ activity, conversationId, messageId }: { activity: AssistantActivity[]; conversationId: string; messageId: string }) {
  const theme = useTheme();
  const c = theme.colors;
  if (activity.length === 0) return null;
  return (
    <View accessibilityLabel="Tools used" style={{ gap: theme.space[1] }}>
      {activity.map((a) => {
        if (a.status === "awaiting_approval") return <ApprovalCard key={a.call_id} activity={a} conversationId={conversationId} messageId={messageId} />;
        const icon =
          a.status === "running" ? (
            <Spinner size={14} label={`Running ${assistantToolLabel(a.name)}`} />
          ) : a.status === "ok" ? (
            <CircleCheck size={16} color={c.successStrong} accessibilityLabel="Done" />
          ) : a.status === "denied" ? (
            <Ban size={16} color={c.textSubtle} accessibilityLabel="Not run" />
          ) : a.status === "suggested" ? (
            <ClipboardCheck size={16} color={c.infoStrong} accessibilityLabel="Suggested" />
          ) : (
            <CircleAlert size={16} color={c.dangerStrong} accessibilityLabel="Failed" />
          );
        return (
          <View key={a.call_id} style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2], flexWrap: "wrap" }}>
            {icon}
            {a.source ? (
              <Badge size="sm" tone="neutral">
                {a.source}
              </Badge>
            ) : null}
            <Text role="caption" weight={600}>
              {assistantToolLabel(a.name)}
            </Text>
            {a.status === "suggested" && a.suggestion_id ? (
              <Pressable
                accessibilityRole="link"
                hitSlop={12}
                onPress={() => router.push(assistantSuggestionHref(a.suggestion_id ?? "") as Href)}
              >
                <Text role="caption" weight={600} style={{ color: c.infoStrong }}>
                  Suggested — waiting for your review
                </Text>
              </Pressable>
            ) : a.summary ? (
              <Text role="caption" tone={a.status === "error" ? "danger" : "muted"} numberOfLines={1} style={{ flexShrink: 1 }}>
                {a.summary}
              </Text>
            ) : null}
          </View>
        );
      })}
    </View>
  );
}

function ApprovalCard({ activity: a, conversationId, messageId }: { activity: AssistantActivity; conversationId: string; messageId: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const decide = useDecideAssistantToolCall();
  const send = (approved: boolean) =>
    decide.mutate(
      { conversationId, messageId, callId: a.call_id, approved },
      { onError: (e) => toast.error(errorMessage(e, "Couldn't send your answer. Try again.")) },
    );
  return (
    <View
      accessibilityLiveRegion="polite"
      style={{ gap: s[3], borderWidth: 1, borderColor: c.borderStrong, backgroundColor: c.surface, borderRadius: theme.radius.lg, padding: s[4], marginVertical: s[1] }}
    >
      <View style={{ flexDirection: "row", gap: s[2], alignItems: "flex-start" }}>
        <ShieldQuestion size={16} color={c.warningStrong} style={{ marginTop: s[0.5] }} />
        <Text role="label" style={{ flex: 1 }}>{`Allow ${assistantToolLabel(a.name)} in ${a.source ?? "the connected service"}? This changes something there.`}</Text>
      </View>
      <ScrollView style={{ maxHeight: 192, backgroundColor: c.sunken, borderRadius: theme.radius.md }} contentContainerStyle={{ padding: s[3] }}>
        <Text role="caption" selectable style={{ fontFamily: MONO }}>
          {assistantToolInputText(a.input)}
        </Text>
      </ScrollView>
      <View style={{ flexDirection: "row", gap: s[2] }}>
        <Button size="md" variant="secondary" disabled={decide.isPending} onPress={() => send(false)} style={{ flex: 1 }}>
          Decline
        </Button>
        <Button size="md" loading={decide.isPending && decide.variables?.approved === true} disabled={decide.isPending} onPress={() => send(true)} style={{ flex: 1 }}>
          Approve
        </Button>
      </View>
    </View>
  );
}
