import { CloudOff, Inbox, RotateCw } from "lucide-react-native";
import { useEffect, type ReactNode } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSequence, withTiming } from "react-native-reanimated";
import { renderIcon, type IconLike } from "../lib/icon";
import { enterUp, popIn } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Button } from "./Button";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export interface EmptyStateProps {
  icon?: IconLike;
  /** Short title ("No messages yet"). No paragraph: context goes in `info`. */
  title: string;
  info?: ReactNode;
  /** The one action ("Start a conversation"). */
  action?: ReactNode;
  compact?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** Icon tile, title, one action. */
export function EmptyState({ icon = Inbox, title, info, action, compact, style }: EmptyStateProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const tile = compact ? s[12] : s[16];
  return (
    <View style={[{ alignItems: "center", justifyContent: "center", gap: compact ? s[3] : s[4], paddingVertical: compact ? s[8] : s[12] }, style]}>
      <Animated.View entering={popIn(theme)} style={{ width: tile, height: tile, borderRadius: theme.radius.xl, alignItems: "center", justifyContent: "center", backgroundColor: c.sunken }}>
        {renderIcon(icon, { size: tile / 2, color: c.textMuted })}
      </Animated.View>
      <Animated.View entering={enterUp(theme, 1)} style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap", justifyContent: "center" }}>
        <Text role={compact ? "titleSm" : "title"} align="center" accessibilityRole="header">
          {title}
        </Text>
        {info ? <InfoTip content={info} size="md" /> : null}
      </Animated.View>
      {action ? <Animated.View entering={enterUp(theme, 2)}>{action}</Animated.View> : null}
    </View>
  );
}

export interface ErrorStateProps {
  /** What failed ("Couldn't load your profile"). */
  title?: string;
  /** Details (the API's message) behind the ⓘ. */
  info?: ReactNode;
  onRetry?: () => void;
  /** @default "Try again" */
  retryLabel?: string;
  retrying?: boolean;
  icon?: IconLike;
  compact?: boolean;
  style?: StyleProp<ViewStyle>;
}

/** What failed + Retry. A short shake on mount (skipped under reduced motion). */
export function ErrorState({ title = "Couldn't load this", info, onRetry, retryLabel = "Try again", retrying, icon = CloudOff, compact, style }: ErrorStateProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const tile = compact ? s[12] : s[16];
  const x = useSharedValue(0);
  useEffect(() => {
    x.value = withSequence(withTiming(-6, { duration: 60 }), withTiming(6, { duration: 80 }), withTiming(-3, { duration: 70 }), withTiming(0, { duration: 60 }));
  }, [x]);
  const shake = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }] }));
  return (
    <View accessibilityRole="alert" style={[{ alignItems: "center", justifyContent: "center", gap: compact ? s[3] : s[4], paddingVertical: compact ? s[8] : s[12] }, style]}>
      <Animated.View entering={popIn(theme)} style={[{ width: tile, height: tile, borderRadius: theme.radius.xl, alignItems: "center", justifyContent: "center", backgroundColor: c.dangerSoft }, shake]}>
        {renderIcon(icon, { size: tile / 2, color: c.dangerStrong })}
      </Animated.View>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap", justifyContent: "center" }}>
        <Text role={compact ? "titleSm" : "title"} align="center">
          {title}
        </Text>
        {info ? <InfoTip content={info} size="md" label="Error details" /> : null}
      </View>
      {onRetry ? (
        <Button variant="outline" leftIcon={RotateCw} onPress={onRetry} loading={retrying}>
          {retryLabel}
        </Button>
      ) : null}
    </View>
  );
}
