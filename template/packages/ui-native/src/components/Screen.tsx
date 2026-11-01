import { forwardRef, useCallback, useEffect, useRef, useState, type ReactElement, type ReactNode } from "react";
import { Keyboard, KeyboardAvoidingView, ScrollView, View, type RefreshControlProps, type ScrollViewProps, type StyleProp, type ViewStyle } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useLayout, useTheme } from "../theme/ThemeProvider";

/** Safe-area edges a screen pads its content for. Left and right are always applied. */
export type ScreenEdge = "top" | "bottom";

export interface ScreenProps {
  children: ReactNode;
  /** Scroll the content (default). `false` for screens that manage their own list. @default true */
  scroll?: boolean;
  /**
   * Insets added to the content padding. Default both; drop `top` under a native header
   * and `bottom` inside tabs (the tab bar owns it).
   */
  edges?: ScreenEdge[];
  /** Gutter on the sides, breathing room top and bottom. @default true */
  padded?: boolean;
  /** Gap between direct children (sections). @default the layout's section gap (32 on phones) */
  gap?: number;
  background?: "canvas" | "surface";
  /** Shrink above the keyboard so the focused field and the footer stay visible (forms). */
  keyboard?: boolean;
  /** Distance from the window top to this screen; measured automatically when omitted. */
  keyboardOffset?: number;
  refreshControl?: ReactElement<RefreshControlProps>;
  /** Sticky bottom area for the screen's one primary action. */
  footer?: ReactNode;
  /** Center the content vertically (sign-in, empty app, update screen). */
  centered?: boolean;
  contentStyle?: StyleProp<ViewStyle>;
  scrollProps?: Omit<ScrollViewProps, "children" | "contentContainerStyle" | "refreshControl">;
}

function useKeyboardVisible(enabled: boolean): boolean {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!enabled) return undefined;
    const show = Keyboard.addListener("keyboardDidShow", () => setVisible(true));
    const hide = Keyboard.addListener("keyboardDidHide", () => setVisible(false));
    return () => {
      show.remove();
      hide.remove();
    };
  }, [enabled]);
  return visible;
}

/**
 * Screen frame. The canvas runs edge to edge (Android 16 is always edge-to-edge); safe-area
 * insets go on the content padding, never a wrapper, so scrolled content passes under the
 * status bar and the last item clears the home indicator. Content is capped at the
 * layout's reading width on tablets. Pass a `ref` to reach the ScrollView
 * (`useScrollToTop(ref)` in tab screens).
 */
export const Screen = forwardRef<ScrollView, ScreenProps>(function Screen(
  { children, scroll = true, edges = ["top", "bottom"], padded = true, gap, background = "canvas", keyboard, keyboardOffset, refreshControl, footer, centered, contentStyle, scrollProps },
  ref,
) {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const c = theme.colors;
  const s = theme.space;
  const keyboardUp = useKeyboardVisible(Boolean(keyboard));
  const rootRef = useRef<View>(null);
  const [measuredOffset, setMeasuredOffset] = useState(0);
  const measure = useCallback(() => {
    if (!keyboard || keyboardOffset !== undefined) return;
    rootRef.current?.measureInWindow((_x, y) => setMeasuredOffset(y));
  }, [keyboard, keyboardOffset]);

  const top = edges.includes("top") ? insets.top : 0;
  const bottom = edges.includes("bottom") && !keyboardUp ? insets.bottom : 0;
  const side = padded ? layout.gutter : 0;
  const content: ViewStyle = {
    gap: gap ?? layout.section,
    paddingTop: top + (padded ? s[4] : 0),
    paddingBottom: footer ? (padded ? s[6] : 0) : bottom + (padded ? s[8] : 0),
    paddingLeft: insets.left + side,
    paddingRight: insets.right + side,
    width: "100%",
    maxWidth: layout.maxContent + insets.left + insets.right + side * 2,
    alignSelf: "center",
    ...(centered ? { flexGrow: 1, justifyContent: "center" } : null),
  };

  const body = scroll ? (
    <ScrollView
      ref={ref}
      style={{ flex: 1 }}
      contentContainerStyle={[{ flexGrow: 1 }, content, contentStyle]}
      keyboardShouldPersistTaps="handled"
      keyboardDismissMode="interactive"
      contentInsetAdjustmentBehavior="never"
      {...(refreshControl ? { refreshControl } : {})}
      {...scrollProps}
    >
      {children}
    </ScrollView>
  ) : (
    <View style={[{ flex: 1 }, content, contentStyle]}>{children}</View>
  );

  const footerView = footer ? (
    <View
      style={{
        paddingTop: s[3],
        paddingBottom: bottom + s[3],
        paddingLeft: insets.left + layout.gutter,
        paddingRight: insets.right + layout.gutter,
        borderTopWidth: 1,
        borderTopColor: c.border,
        backgroundColor: c[background],
      }}
    >
      <View style={{ width: "100%", maxWidth: layout.maxContent, alignSelf: "center" }}>{footer}</View>
    </View>
  ) : null;

  return (
    <View ref={rootRef} onLayout={measure} style={{ flex: 1, backgroundColor: c[background] }}>
      {keyboard ? (
        <KeyboardAvoidingView style={{ flex: 1 }} behavior="padding" keyboardVerticalOffset={keyboardOffset ?? measuredOffset}>
          {body}
          {footerView}
        </KeyboardAvoidingView>
      ) : (
        <>
          {body}
          {footerView}
        </>
      )}
    </View>
  );
});
