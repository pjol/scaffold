import { ChevronDown } from "lucide-react-native";
import { Fragment, useRef, useState, type ReactNode } from "react";
import { Linking, Pressable, ScrollView, View, useWindowDimensions, type StyleProp, type ViewStyle } from "react-native";
import Animated, { useReducedMotion } from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { fadeIn, fadeOut, layoutSpring } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Badge } from "./Badge";
import { InfoTip } from "./InfoTip";
import { Text } from "./Text";

export type LegalBlock = { type: "paragraph"; text: string } | { type: "bullets"; items: string[] };

export interface LegalSection {
  id: string;
  heading: string;
  blocks: LegalBlock[];
}

/** A versioned document; `@scaffoldapp/shared/legal` documents fit this shape. */
export interface LegalDocumentData {
  version: string;
  title: string;
  /** `YYYY-MM-DD`. */
  effective: string;
  sections: LegalSection[];
}

export interface LegalDocumentProps {
  document: LegalDocumentData;
  /** Collapsible contents that jump to sections. @default true */
  toc?: boolean;
  /** Title row (hide inside a sheet or under a header that shows it). @default true */
  showTitle?: boolean;
  /** @default true */
  numbered?: boolean;
  /** Own ScrollView (needed for the contents jumps). @default true */
  scrollable?: boolean;
  footer?: ReactNode;
  style?: StyleProp<ViewStyle>;
  contentContainerStyle?: StyleProp<ViewStyle>;
}

/** "2026-10-01" → "October 1, 2026". */
export function formatEffectiveDate(effective: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(effective.trim());
  if (!m) return effective;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return d.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" });
}

const LINK_RE = /(https?:\/\/[^\s)]+[^\s).,;:!?]|[\w.+-]+@[\w-]+\.[\w.-]*\w)/g;

function Linkified({ text }: { text: string }) {
  const parts = text.split(LINK_RE);
  return (
    <>
      {parts.map((part, k) =>
        k % 2 === 1 ? (
          <Text
            key={k}
            tone="primary"
            weight={600}
            accessibilityRole="link"
            onPress={() => void Linking.openURL(part.includes("@") && !part.startsWith("http") ? `mailto:${part}` : part).catch(() => undefined)}
            style={{ textDecorationLine: "underline" }}
          >
            {part}
          </Text>
        ) : (
          <Fragment key={k}>{part}</Fragment>
        ),
      )}
    </>
  );
}

/** Legal reading: numbered headings, body text, bullets, tappable links and emails, a contents list. */
export function LegalDocument({ document: doc, toc = true, showTitle = true, numbered = true, scrollable = true, footer, style, contentContainerStyle }: LegalDocumentProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const reduce = useReducedMotion();
  const { fontScale } = useWindowDimensions();
  const scroll = useRef<ScrollView>(null);
  const offsets = useRef<Record<string, number>>({});
  const [open, setOpen] = useState(false);
  const label = (k: number, heading: string) => (numbered ? `${k + 1}. ${heading}` : heading);

  const jump = (id: string) => {
    haptic.selection();
    setOpen(false);
    const y = offsets.current[id];
    if (y !== undefined) scroll.current?.scrollTo({ y: Math.max(0, y - s[2]), animated: !reduce });
  };

  const body = (
    <View style={{ gap: s[8] }}>
      <View style={{ gap: s[3] }}>
        {showTitle ? (
          <Text role="title" accessibilityRole="header">
            {doc.title}
          </Text>
        ) : null}
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Badge>{`Effective ${formatEffectiveDate(doc.effective)}`}</Badge>
          <InfoTip content={`Version ${doc.version}`} label="Document version" />
        </View>
      </View>

      {toc && scrollable && doc.sections.length > 1 ? (
        <Animated.View layout={layoutSpring(theme)} style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.lg, backgroundColor: c.surface, overflow: "hidden" }}>
          <Pressable
            onPress={() => setOpen((o) => !o)}
            accessibilityRole="button"
            accessibilityState={{ expanded: open }}
            style={{ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: s[3], paddingHorizontal: s[4], paddingVertical: s[2] }}
          >
            <Text weight={600}>Contents</Text>
            <View style={{ transform: [{ rotate: open ? "180deg" : "0deg" }] }}>
              <ChevronDown size={20} color={c.textMuted} strokeWidth={2.25} />
            </View>
          </Pressable>
          {open ? (
            <Animated.View entering={fadeIn(theme)} exiting={fadeOut(theme)} style={{ borderTopWidth: 1, borderTopColor: c.border, paddingVertical: s[1] }}>
              {doc.sections.map((sec, k) => (
                <Pressable
                  key={sec.id}
                  onPress={() => jump(sec.id)}
                  accessibilityRole="link"
                  style={({ pressed }) => ({ minHeight: theme.tap.base, justifyContent: "center", paddingHorizontal: s[4], paddingVertical: s[2], backgroundColor: pressed ? c.sunken : "transparent" })}
                >
                  <Text tone="muted">{label(k, sec.heading)}</Text>
                </Pressable>
              ))}
            </Animated.View>
          ) : null}
        </Animated.View>
      ) : null}

      {doc.sections.map((sec, k) => (
        <View
          key={sec.id}
          onLayout={(e) => {
            offsets.current[sec.id] = e.nativeEvent.layout.y;
          }}
          style={{ gap: s[3] }}
        >
          <Text role="titleSm" accessibilityRole="header">
            {label(k, sec.heading)}
          </Text>
          {sec.blocks.map((b, j) =>
            b.type === "paragraph" ? (
              <Text key={j}>
                <Linkified text={b.text} />
              </Text>
            ) : (
              <View key={j} style={{ gap: s[2] }}>
                {b.items.map((item, n) => (
                  <View key={n} style={{ flexDirection: "row", gap: s[3], paddingLeft: s[1] }}>
                    {/* Dot centred on the first line at the current text size. */}
                    <View style={{ height: theme.type.body.lineHeight * fontScale, justifyContent: "center" }}>
                      <View style={{ width: s[1.5], height: s[1.5], borderRadius: s[1], backgroundColor: c.textMuted }} />
                    </View>
                    <Text style={{ flex: 1 }}>
                      <Linkified text={item} />
                    </Text>
                  </View>
                ))}
              </View>
            ),
          )}
        </View>
      ))}
      {footer}
    </View>
  );

  if (!scrollable) return <View style={style}>{body}</View>;
  return (
    <ScrollView ref={scroll} style={style} contentContainerStyle={[{ paddingBottom: s[8] }, contentContainerStyle]}>
      {body}
    </ScrollView>
  );
}
