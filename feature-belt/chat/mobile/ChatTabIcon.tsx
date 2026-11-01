import { useChatUnreadCount } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import { MessagesSquare } from "lucide-react-native";
import { View, type ColorValue } from "react-native";

function Icon({ color, size }: { color: ColorValue; size: number }) {
  const theme = useTheme();
  const c = theme.colors;
  const unread = useChatUnreadCount() ?? 0;
  return (
    <View>
      <MessagesSquare color={typeof color === "string" ? color : undefined} size={size} strokeWidth={2} />
      {unread > 0 ? (
        <View
          accessibilityLabel={`${unread} unread`}
          style={{
            position: "absolute",
            top: -theme.space[1],
            right: -theme.space[2],
            minWidth: theme.space[4],
            paddingHorizontal: theme.space[1],
            borderRadius: theme.radius.pill,
            backgroundColor: c.danger,
            borderWidth: 2,
            borderColor: c.surface,
            alignItems: "center",
          }}
        >
          <Text role="caption" weight={600} style={{ color: c.dangerFg }} maxFontSizeMultiplier={1.5}>
            {unread > 99 ? "99+" : String(unread)}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

/** Tab icon with the unread message count (`tabBarIcon` is called as a function, so the hook lives in a component). */
export function ChatTabIcon({ color, size }: { color: ColorValue; size: number; focused: boolean }) {
  return <Icon color={color} size={size} />;
}
