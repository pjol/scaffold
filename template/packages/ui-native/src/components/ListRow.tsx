import { ChevronRight } from "lucide-react-native";
import { Children, createContext, Fragment, isValidElement, useContext, type ReactNode } from "react";
import { Pressable, StyleSheet, View, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { usePressScale } from "../lib/press";
import { useTheme } from "../theme/ThemeProvider";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

const InListGroup = createContext(false);

/** A row's side inset: 16 on its own, 0 in a `ListGroup`, whose rows span the column. */
export function useRowInset(): number {
  const inset = useTheme().space[4];
  return useContext(InListGroup) ? 0 : inset;
}

export interface ListRowProps {
  /** Row title. There is intentionally no subtitle prop: context goes in `info`. */
  title: string;
  /** Icon or avatar; the row grows to 56. */
  leading?: ReactNode;
  /** Value, badge or switch on the right. */
  trailing?: ReactNode;
  info?: ReactNode;
  onPress?: () => void;
  /** Chevron (defaults on when pressable). */
  chevron?: boolean;
  destructive?: boolean;
  disabled?: boolean;
  accessibilityHint?: string;
  style?: StyleProp<ViewStyle>;
}

/** One-line row: leading · title (+ⓘ) · trailing · chevron. 48 tall (56 with leading), wraps at large text. */
export function ListRow({ title, leading, trailing, info, onPress, chevron, destructive, disabled, accessibilityHint, style }: ListRowProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const press = usePressScale(0.99);
  const inset = useRowInset();
  const row = (
    <Animated.View
      style={[
        { flexDirection: "row", alignItems: "center", gap: s[3], minHeight: leading ? theme.tap.lg : theme.tap.base, paddingVertical: s[2], paddingHorizontal: inset, opacity: disabled ? 0.5 : 1 },
        onPress ? press.style : null,
        style,
      ]}
    >
      {leading ? <View>{leading}</View> : null}
      <View style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Text weight={500} tone={destructive ? "danger" : "default"} style={{ flexShrink: 1 }}>
          {title}
        </Text>
        {info ? <InfoTip content={info} label={`About ${title}`} /> : null}
      </View>
      {trailing ? <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexShrink: 1 }}>{trailing}</View> : null}
      {(chevron ?? Boolean(onPress)) ? <ChevronRight size={20} color={c.textSubtle} strokeWidth={2.25} /> : null}
    </Animated.View>
  );
  if (!onPress) return row;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={title}
      {...(accessibilityHint ? { accessibilityHint } : {})}
      accessibilityState={{ disabled: Boolean(disabled) }}
      disabled={disabled}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      onPress={() => {
        haptic.light();
        onPress();
      }}
      android_ripple={{ color: c.sunken }}
    >
      {row}
    </Pressable>
  );
}

/** Hairline divider; `inset` aligns it with the row text. */
export function Divider({ inset = 0, style }: { inset?: number; style?: StyleProp<ViewStyle> }) {
  const c = useTheme().colors;
  return <View style={[{ height: StyleSheet.hairlineWidth, backgroundColor: c.border, marginLeft: inset }, style]} />;
}

export interface ListGroupProps {
  /** Group heading (sentence case). */
  title?: string;
  info?: ReactNode;
  /** Rows; hairlines are drawn between them. */
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/** Rows on the canvas: optional heading, then rows that span the column with hairlines between. No box of its own, so the column doesn't shrink (design.md, house rule 8). */
export function ListGroup({ title, info, children, style }: ListGroupProps) {
  const s = useTheme().space;
  const rows = Children.toArray(children).filter((child) => isValidElement(child));
  return (
    <View style={[{ gap: s[2] }, style]}>
      {title ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" tone="muted" accessibilityRole="header">
            {title}
          </Text>
          {info ? <InfoTip content={info} label={`About ${title}`} /> : null}
        </View>
      ) : null}
      <InListGroup.Provider value={true}>
        <View>
          {rows.map((row, i) => (
            <Fragment key={(isValidElement(row) && row.key) || i}>
              {i > 0 ? <Divider /> : null}
              {row}
            </Fragment>
          ))}
        </View>
      </InListGroup.Provider>
    </View>
  );
}
