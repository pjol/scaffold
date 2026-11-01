import { assistantToolLabel, type AssistantActivity } from "@scaffoldapp/shared";
import { Spinner, Text, useTheme } from "@scaffoldapp/ui-native";
import { CircleAlert, CircleCheck } from "lucide-react-native";
import { View } from "react-native";

/** Tool calls behind a reply, one compact line each. */
export function ToolActivity({ activity }: { activity: AssistantActivity[] }) {
  const theme = useTheme();
  const c = theme.colors;
  if (activity.length === 0) return null;
  return (
    <View accessibilityLabel="Tools used" style={{ gap: theme.space[1] }}>
      {activity.map((a) => (
        <View key={a.call_id} style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          {a.status === "running" ? (
            <Spinner size={14} label={`Running ${assistantToolLabel(a.name)}`} />
          ) : a.status === "ok" ? (
            <CircleCheck size={16} color={c.successStrong} accessibilityLabel="Done" />
          ) : (
            <CircleAlert size={16} color={c.dangerStrong} accessibilityLabel="Failed" />
          )}
          <Text role="caption" weight={600}>
            {assistantToolLabel(a.name)}
          </Text>
          {a.summary ? (
            <Text role="caption" tone={a.status === "error" ? "danger" : "muted"} numberOfLines={1} style={{ flexShrink: 1 }}>
              {a.summary}
            </Text>
          ) : null}
        </View>
      ))}
    </View>
  );
}
