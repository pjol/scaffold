import { useChannelsMentionCount } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import { Hash } from "lucide-react-native";
import { View, type ColorValue } from "react-native";

function Icon({ color, size }: { color: ColorValue; size: number }) {
  const theme = useTheme();
  const c = theme.colors;
  const count = useChannelsMentionCount() ?? 0;
  return (
    <View>
      <Hash color={typeof color === "string" ? color : undefined} size={size} strokeWidth={2} />
      {count > 0 ? (
        <View
          accessibilityLabel={`${count} mentions`}
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
            {count > 99 ? "99+" : String(count)}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

/** Tab icon with the mentions and DM messages waiting (`tabBarIcon` is called as a function, so the hook lives in a component). */
export function ChannelsTabIcon({ color, size }: { color: ColorValue; size: number; focused: boolean }) {
  return <Icon color={color} size={size} />;
}
