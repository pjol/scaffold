import { useEffect, useState, type ReactNode } from "react";
import { Pressable, StyleSheet, View, type LayoutChangeEvent, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { haptic } from "../lib/haptics";
import { useTheme } from "../theme/ThemeProvider";
import { Text } from "./Text";

/** The structural subset of React Navigation's `BottomTabBarProps` (expo-router `Tabs`) the bar reads. */
export interface TabBarProps {
  state: { index: number; routes: readonly { key: string; name: string; params?: object | undefined }[] };
  descriptors: Record<
    string,
    {
      options: {
        title?: string | undefined;
        tabBarLabel?: unknown;
        tabBarIcon?: ((props: { focused: boolean; color: string; size: number }) => ReactNode) | undefined;
        tabBarBadge?: string | number | undefined;
        tabBarAccessibilityLabel?: string | undefined;
        /** expo-router turns `href: null` into `display: "none"` here. */
        tabBarItemStyle?: StyleProp<ViewStyle>;
      };
    }
  >;
  navigation: {
    emit: (event: { type: "tabPress"; target: string; canPreventDefault: true }) => { defaultPrevented: boolean };
    navigate: (name: string, params?: object) => void;
  };
}

const ICON = 24;

/**
 * Tab bar for expo-router: `<Tabs tabBar={(p) => <TabBar {...p} />}>`. Labelled icons,
 * one spring-gliding indicator, badges, a selection haptic, and the bottom safe area.
 * Its height comes from its content (tap target + label at the current text size),
 * never a constant. Re-tapping the active tab emits `tabPress`, so `useScrollToTop` and
 * nested stacks scroll or pop to the top.
 */
export function TabBar({ state, descriptors, navigation }: TabBarProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const insets = useSafeAreaInsets();
  const routes = state.routes.filter((r) => StyleSheet.flatten(descriptors[r.key]?.options.tabBarItemStyle)?.display !== "none");
  const activeKey = state.routes[state.index]?.key;
  const activeIdx = Math.max(0, routes.findIndex((r) => r.key === activeKey));
  const [width, setWidth] = useState(0);
  const itemW = routes.length ? width / routes.length : 0;
  const x = useSharedValue(0);
  const spring = theme.motion.spring.snappy;
  useEffect(() => {
    x.value = withSpring(activeIdx * itemW, spring);
  }, [activeIdx, itemW, x, spring]);
  const indicator = useAnimatedStyle(() => ({ transform: [{ translateX: x.value }] }));

  return (
    <View
      accessibilityRole="tablist"
      style={{ backgroundColor: c.surface, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: c.border, paddingBottom: insets.bottom, paddingLeft: insets.left, paddingRight: insets.right }}
    >
      <View style={{ flexDirection: "row" }} onLayout={(e: LayoutChangeEvent) => setWidth(e.nativeEvent.layout.width)}>
        {itemW > 0 ? (
          <Animated.View pointerEvents="none" style={[{ position: "absolute", top: s[1.5], width: itemW, alignItems: "center" }, indicator]}>
            <View style={{ width: s[16], height: s[8], borderRadius: theme.radius.pill, backgroundColor: c.primarySoft }} />
          </Animated.View>
        ) : null}
        {routes.map((route) => {
          const opts = descriptors[route.key]?.options ?? {};
          const focused = route.key === activeKey;
          const color = focused ? c.primaryStrong : c.textMuted;
          const label = typeof opts.tabBarLabel === "string" ? opts.tabBarLabel : (opts.title ?? route.name);
          const badge = opts.tabBarBadge;
          return (
            <Pressable
              key={route.key}
              accessibilityRole="tab"
              accessibilityState={{ selected: focused }}
              accessibilityLabel={opts.tabBarAccessibilityLabel ?? (badge !== undefined ? `${label}, ${badge}` : label)}
              onPress={() => {
                const event = navigation.emit({ type: "tabPress", target: route.key, canPreventDefault: true });
                if (!focused && !event.defaultPrevented) {
                  haptic.selection();
                  navigation.navigate(route.name, route.params);
                }
              }}
              style={{ flex: 1, minHeight: theme.tap.base, alignItems: "center", justifyContent: "center", gap: s[1], paddingTop: s[1.5], paddingBottom: s[1.5] }}
            >
              <View style={{ height: s[8], minWidth: s[16], alignItems: "center", justifyContent: "center" }}>
                {opts.tabBarIcon?.({ focused, color, size: ICON })}
                {badge !== undefined ? (
                  <View
                    style={{
                      position: "absolute",
                      top: 0,
                      left: s[8] + s[2],
                      minWidth: s[5],
                      paddingHorizontal: s[1],
                      borderRadius: theme.radius.pill,
                      backgroundColor: c.danger,
                      borderWidth: 2,
                      borderColor: c.surface,
                      alignItems: "center",
                    }}
                  >
                    <Text role="caption" weight={700} maxFontSizeMultiplier={1.25} style={{ color: c.dangerFg }}>
                      {String(badge)}
                    </Text>
                  </View>
                ) : null}
              </View>
              <Text role="caption" weight={focused ? 700 : 500} maxFontSizeMultiplier={1.5} numberOfLines={1} style={{ color }}>
                {label}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}
