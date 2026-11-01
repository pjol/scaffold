import { useFeedUnreadCount } from "@scaffoldapp/shared";
import { useTheme } from "@scaffoldapp/ui-native";
import { Newspaper } from "lucide-react-native";
import { View, type ColorValue } from "react-native";

/**
 * Tab icon for the Feed tab, with a dot while there are unread posts. It is a component
 * (rendered as `<FeedTabIcon {...props} />`) so the unread hook runs in its own render.
 */
export function FeedTabIcon({ color, size }: { focused: boolean; color: ColorValue; size: number }) {
  const theme = useTheme();
  const unread = useFeedUnreadCount();
  return (
    <View accessibilityLabel={unread ? "Feed, new posts" : undefined}>
      <Newspaper color={color as string} size={size} strokeWidth={2} />
      {unread ? (
        <View
          style={{
            position: "absolute",
            top: -theme.space[0.5],
            right: -theme.space[1],
            width: theme.space[3],
            height: theme.space[3],
            borderRadius: theme.radius.pill,
            backgroundColor: theme.colors.danger,
            borderWidth: 2,
            borderColor: theme.colors.canvas,
          }}
        />
      ) : null}
    </View>
  );
}
