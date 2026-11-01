import { agentsDiff, agentsDiffHunks, agentsInputRows, type AgentsDiffLine, type AgentsSuggestion } from "@scaffoldapp/shared";
import { Text, useTheme } from "@scaffoldapp/ui-native";
import { useMemo, useState } from "react";
import { Pressable, View } from "react-native";

const MARK: Record<AgentsDiffLine["op"], string> = { same: " ", del: "−", add: "+" };
const SPOKEN: Record<AgentsDiffLine["op"], string> = { same: "", del: "Removed: ", add: "Added: " };

/** Before → after: removed lines on a danger tint with "−", added on a success tint with "+", changed words bold. */
export function SuggestionDiff({ before, after }: { before: string; after: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [expanded, setExpanded] = useState(false);
  const lines = useMemo(() => agentsDiff(before, after), [before, after]);
  const rows = expanded ? lines : agentsDiffHunks(lines);
  return (
    <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md, overflow: "hidden", paddingVertical: s[1] }} accessibilityLabel="Changes">
      {rows.map((row, i) =>
        "hidden" in row ? (
          <Pressable
            key={`h${i}`}
            onPress={() => setExpanded(true)}
            accessibilityRole="button"
            style={{ paddingHorizontal: s[3], paddingVertical: s[2], minHeight: 44, justifyContent: "center" }}
          >
            <Text role="caption" tone="muted">
              {row.hidden} unchanged {row.hidden === 1 ? "line" : "lines"}
            </Text>
          </Pressable>
        ) : (
          <View
            key={i}
            accessible
            accessibilityLabel={SPOKEN[row.op] + row.parts.map((p) => p.text).join("")}
            style={{
              flexDirection: "row",
              gap: s[2],
              paddingHorizontal: s[3],
              paddingVertical: s[0.5],
              backgroundColor: row.op === "del" ? c.dangerSoft : row.op === "add" ? c.successSoft : "transparent",
            }}
          >
            <Text role="label" style={{ width: 12, color: row.op === "del" ? c.dangerStrong : row.op === "add" ? c.successStrong : c.textMuted }}>
              {MARK[row.op]}
            </Text>
            <Text role="label" tone={row.op === "same" ? "muted" : "default"} style={{ flex: 1 }}>
              {row.parts.map((p, j) => (
                <Text
                  key={j}
                  role="label"
                  {...(p.op === "same" ? {} : { weight: 700 as const })}
                  style={
                    p.op === "same"
                      ? undefined
                      : { textDecorationLine: p.op === "del" ? "line-through" : "underline", color: p.op === "del" ? c.dangerStrong : c.successStrong }
                  }
                >
                  {p.text}
                </Text>
              ))}
            </Text>
          </View>
        ),
      )}
    </View>
  );
}

/** A suggestion without before/after: what the agent would send, as labelled rows. */
export function SuggestionInput({ input }: { input: Record<string, unknown> }) {
  const theme = useTheme();
  const rows = agentsInputRows(input);
  if (rows.length === 0) return null;
  return (
    <View style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md, padding: theme.space[4], gap: theme.space[3] }}>
      {rows.map((r) => (
        <View key={r.key} style={{ gap: theme.space[1] }}>
          <Text role="label" tone="muted">
            {r.key.charAt(0).toUpperCase() + r.key.slice(1)}
          </Text>
          <Text>{r.value}</Text>
        </View>
      ))}
    </View>
  );
}

export function SuggestionChange({ s }: { s: AgentsSuggestion }) {
  if (s.before || s.after) return <SuggestionDiff before={s.before} after={s.after} />;
  return <SuggestionInput input={s.input} />;
}
