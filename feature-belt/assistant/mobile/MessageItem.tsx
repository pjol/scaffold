import { assistantMessageText, assistantStopNote, type AssistantMessage } from "@scaffoldapp/shared";
import { Badge, Banner, IconButton, InfoTip, Spinner, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Flag, Share2 } from "lucide-react-native";
import { useState } from "react";
import { Share, View } from "react-native";
import { ReportDialog } from "@/features/moderation/ReportDialog";
import { Markdown } from "./Markdown";
import { ToolActivity } from "./ToolActivity";

/** One message: the user's text in a bubble on the right; replies full width with actions. */
export function MessageItem({ message, onRetry }: { message: AssistantMessage; onRetry?: (() => void) | undefined }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  if (message.role === "user") {
    return (
      <View style={{ alignItems: "flex-end", paddingLeft: s[10] }}>
        <View style={{ backgroundColor: c.primarySoft, borderRadius: theme.radius.xl, paddingHorizontal: s[4], paddingVertical: s[3] }}>
          <Text selectable>{assistantMessageText(message)}</Text>
        </View>
      </View>
    );
  }
  return <Reply message={message} onRetry={onRetry} />;
}

function Reply({ message, onRetry }: { message: AssistantMessage; onRetry?: (() => void) | undefined }) {
  const theme = useTheme();
  const s = theme.space;
  const [reporting, setReporting] = useState(false);
  const text = assistantMessageText(message);
  const streaming = message.status === "streaming";
  const note = assistantStopNote(message);

  if (message.removed) {
    return (
      <Text role="label" tone="muted" style={{ fontStyle: "italic" }}>
        This reply was removed after a report.
      </Text>
    );
  }
  return (
    <View style={{ gap: s[3] }} accessibilityState={{ busy: streaming }}>
      <ToolActivity activity={message.activity} />
      {text ? <Markdown text={text} /> : streaming && message.activity.length === 0 ? <Spinner size={18} label="Writing a reply" /> : null}
      {message.error ? (
        <Banner tone="danger" title={message.error.message} {...(message.error.retryable && onRetry ? { action: { label: "Retry", onPress: onRetry } } : {})} />
      ) : null}
      {!streaming && (text || note) ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          {note ? (
            <Text role="caption" tone="muted">
              {note}
            </Text>
          ) : null}
          {text ? (
            <IconButton
              label="Share reply"
              icon={Share2}
              variant="ghost"
              onPress={() => void Share.share({ message: text }).catch(() => toast.error("Couldn't open sharing."))}
            />
          ) : null}
          {text ? <IconButton label="Report reply" icon={Flag} variant="ghost" onPress={() => setReporting(true)} /> : null}
          {message.provider === "mock" ? (
            <View style={{ marginLeft: "auto", flexDirection: "row", alignItems: "center", gap: s[1] }}>
              <Badge tone="warning" size="sm">
                Mock
              </Badge>
              <InfoTip content="Scripted reply from the development mock. No AI provider was called." />
            </View>
          ) : null}
        </View>
      ) : null}
      <ReportDialog open={reporting} onClose={() => setReporting(false)} targetKind="assistant.message" targetId={message.id} noun="reply" />
    </View>
  );
}
