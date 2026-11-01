import { docsFormatDate, docsTitle, type DocsRefs, type DocsSpan } from "@scaffoldapp/shared";
import { Text, useTheme, type TextProps } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Linking, Platform } from "react-native";

const MONO = Platform.select({ ios: "Menlo", default: "monospace" });

function open(url: string) {
  if (/^(https?:|mailto:)/i.test(url)) void Linking.openURL(url);
}

/** The label a mention shows now (live names; "No access" for pages you can't open). */
export function mentionLabel(sp: DocsSpan, refs: DocsRefs): string {
  const m = sp.mention!;
  if (m.type === "user") return `@${refs.users[m.id]?.name ?? sp.text.replace(/^@/, "")}`;
  if (m.type === "page") {
    const p = refs.pages[m.id];
    if (p && p.title === null) return "No access";
    return p?.title ? `${p.icon ? `${p.icon} ` : ""}${docsTitle(p.title)}` : sp.text;
  }
  return docsFormatDate(m.date) || sp.text;
}

/** Rich text as nested Text: marks, links (http, https, mailto), mention chips. */
export function RichText({ spans, refs, pageHref, ...props }: { spans: readonly DocsSpan[]; refs: DocsRefs; pageHref?: (id: string) => string | null } & TextProps) {
  const theme = useTheme();
  const c = theme.colors;
  return (
    <Text {...props}>
      {spans.map((s, i) => {
        if (s.mention) {
          const label = mentionLabel(s, refs);
          if (s.mention.type === "page") {
            const id = s.mention.id;
            const href = pageHref ? pageHref(id) : `/docs/${id}`;
            const noAccess = refs.pages[id]?.title === null;
            return (
              <Text
                key={i}
                role={props.role ?? "body"}
                style={{ textDecorationLine: noAccess ? "none" : "underline", color: noAccess ? c.textSubtle : c.text }}
                {...(href && !noAccess ? { onPress: () => router.push(href as never), accessibilityRole: "link" as const } : {})}
              >
                {label}
              </Text>
            );
          }
          return (
            <Text key={i} role={props.role ?? "body"} style={{ color: s.mention.type === "user" ? c.primaryStrong : c.textMuted, backgroundColor: s.mention.type === "user" ? c.primarySoft : c.sunken }}>
              {label}
            </Text>
          );
        }
        const deco = [s.underline ? "underline" : "", s.strike ? "line-through" : ""].filter(Boolean).join(" ") as "underline" | "line-through" | "underline line-through" | "";
        return (
          <Text
            key={i}
            role={props.role ?? "body"}
            {...(s.bold ? { weight: 700 as const } : {})}
            style={[
              s.italic ? { fontStyle: "italic" } : null,
              deco ? { textDecorationLine: deco } : null,
              s.code ? { fontFamily: MONO, backgroundColor: c.sunken } : null,
              s.link ? { color: c.primaryStrong, textDecorationLine: "underline" } : null,
            ]}
            {...(s.link ? { onPress: () => open(s.link!), accessibilityRole: "link" as const } : {})}
          >
            {s.text}
          </Text>
        );
      })}
    </Text>
  );
}
