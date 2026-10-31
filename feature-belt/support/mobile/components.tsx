import { SUPPORT_STATUS_LABEL, type SupportMessage, type SupportStatus } from "@scaffoldapp/shared";
import { Badge, Caption, Text, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { FileText } from "lucide-react-native";
import { Linking, Pressable, View } from "react-native";

const STATUS_TONE: Record<SupportStatus, BadgeTone> = { open: "primary", answered: "success", closed: "neutral" };

export function TicketStatusBadge({ status }: { status: SupportStatus }) {
  return (
    <Badge size="sm" tone={STATUS_TONE[status]} dot>
      {SUPPORT_STATUS_LABEL[status]}
    </Badge>
  );
}

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

/** "5 min ago", "yesterday", then the date after 30 days. */
export function ago(iso: string, now: number): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return "just now";
  if (abs < 3600) return relative.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return relative.format(Math.round(s / 3600), "hour");
  if (abs < 30 * 86_400) return relative.format(Math.round(s / 86_400), "day");
  return new Date(iso).toLocaleDateString();
}

/** One thread entry: staff replies on the sunken surface with a "Support" badge. */
export function ThreadMessage({ message, now }: { message: SupportMessage; now: number }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const staff = message.author_role === "staff";
  return (
    <View style={{ gap: s[3], padding: s[4], borderRadius: theme.radius.lg, borderWidth: 1, borderColor: c.border, backgroundColor: staff ? c.sunken : c.surface }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
        <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
          {message.author_name}
        </Text>
        {staff ? (
          <Badge size="sm" tone="primary">
            Support
          </Badge>
        ) : null}
        <Caption tone="muted" style={{ marginLeft: "auto" }}>
          {ago(message.created_at, now)}
        </Caption>
      </View>
      <Text selectable>{message.body}</Text>
      {message.attachments.length > 0 ? (
        <View style={{ gap: s[2] }}>
          {message.attachments.map((a) => (
            <Pressable
              key={a.id}
              accessibilityRole="link"
              accessibilityLabel={`Open ${a.file_name}`}
              onPress={() => void Linking.openURL(a.url)}
              style={({ pressed }) => ({
                flexDirection: "row",
                alignItems: "center",
                gap: s[2],
                minHeight: theme.tap.base,
                paddingHorizontal: s[3],
                borderRadius: theme.radius.md,
                borderWidth: 1,
                borderColor: c.border,
                backgroundColor: pressed ? c.sunken : c.surface,
              })}
            >
              <FileText size={18} color={c.textMuted} strokeWidth={2} />
              <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
                {a.file_name}
              </Text>
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}
