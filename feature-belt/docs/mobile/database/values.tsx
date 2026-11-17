import {
  docsFormatComputed,
  docsFormatPropertyNumber,
  docsFormatUniqueID,
  docsFormatValue,
  docsNumberFill,
  docsOption,
  docsOptionHue,
  docsTitle,
  type DocsComputedValue,
  type DocsDateValue,
  type DocsFileValue,
  type DocsOptionColor,
  type DocsProperty,
  type DocsRefs,
  type DocsRow,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Avatar, FileIcon, Spinner, Text, useTheme } from "@scaffoldapp/ui-native";
import { Bell, Link2, Lock, Square, SquareCheck } from "lucide-react-native";
import type { ReactNode } from "react";
import { Pressable, View } from "react-native";
import Svg, { Circle } from "react-native-svg";

/** What a value needs from its row (created/edited time and people). */
export type DocsRowMeta = Pick<DocsRow, "created_at" | "updated_at" | "created_by" | "updated_by">;

/** Swatch colours an option colour draws with: hue text on a hue background, or a neutral chip. */
export function useOptionColors(color: DocsOptionColor | undefined): { fg: string; bg: string; text: string } {
  const theme = useTheme();
  const hue = docsOptionHue(color);
  const sw = hue ? theme.swatches[hue] : null;
  return sw ? { fg: sw.fg, bg: sw.bg, text: sw.fg } : { fg: theme.colors.textMuted, bg: theme.colors.sunken, text: theme.colors.text };
}

/** A select / status option or a group label as a coloured chip. */
export function OptionChip({ name, color, dot = false, size = "md" }: { name: string; color?: DocsOptionColor | undefined; dot?: boolean; size?: "sm" | "md" }) {
  const theme = useTheme();
  const s = theme.space;
  const col = useOptionColors(color);
  return (
    <View
      style={{
        flexDirection: "row",
        alignItems: "center",
        gap: s[1],
        alignSelf: "flex-start",
        maxWidth: "100%",
        paddingHorizontal: s[2],
        paddingVertical: size === "sm" ? 0 : s[0.5],
        borderRadius: theme.radius.sm,
        backgroundColor: col.bg,
      }}
    >
      {dot ? <View style={{ width: s[2], height: s[2], borderRadius: s[1], backgroundColor: col.fg }} /> : null}
      <Text role={size === "sm" ? "caption" : "label"} weight={500} numberOfLines={1} style={{ color: col.text, flexShrink: 1 }}>
        {name}
      </Text>
    </View>
  );
}

/** An option's colour as a dot (pickers, where the row title carries the name). */
export function OptionDot({ color }: { color?: DocsOptionColor | undefined }) {
  const theme = useTheme();
  const col = useOptionColors(color);
  return <View style={{ width: theme.space[4], height: theme.space[4], borderRadius: theme.radius.sm, backgroundColor: col.bg, borderWidth: 1, borderColor: col.fg }} />;
}

/** A small neutral chip (related rows, files, people). */
function Pill({ children, onPress, label, muted }: { children: ReactNode; onPress?: (() => void) | undefined; label?: string; muted?: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const body = (
    <View
      style={{
        flexDirection: "row",
        alignItems: "center",
        gap: s[1],
        maxWidth: "100%",
        paddingHorizontal: s[2],
        paddingVertical: s[0.5],
        borderRadius: theme.radius.sm,
        backgroundColor: muted ? "transparent" : theme.colors.sunken,
        borderWidth: muted ? 1 : 0,
        borderColor: theme.colors.border,
      }}
    >
      {children}
    </View>
  );
  if (!onPress) return body;
  return (
    <Pressable accessibilityRole="link" {...(label ? { accessibilityLabel: label } : {})} onPress={onPress} hitSlop={theme.space[1]}>
      {body}
    </Pressable>
  );
}

function Wrap({ children }: { children: ReactNode }) {
  const s = useTheme().space;
  return <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[1], alignItems: "center" }}>{children}</View>;
}

/** A number as a bar or a ring beside its label (number_display). */
export function NumberMeter({ prop, value, stored }: { prop: DocsProperty; value: number; stored: boolean }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const fill = docsNumberFill(prop, value, stored);
  const label = docsFormatNumberFor(prop, value, stored);
  if (prop.number_display === "ring") {
    const r = 7;
    const len = 2 * Math.PI * r;
    return (
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }} accessible accessibilityLabel={label}>
        <Svg width={18} height={18} viewBox="0 0 18 18">
          <Circle cx={9} cy={9} r={r} stroke={c.sunken} strokeWidth={3} fill="none" />
          <Circle cx={9} cy={9} r={r} stroke={c.primary} strokeWidth={3} fill="none" strokeDasharray={`${len * fill} ${len}`} strokeLinecap="round" transform="rotate(-90 9 9)" />
        </Svg>
        <Text role="label" tabular>
          {label}
        </Text>
      </View>
    );
  }
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }} accessible accessibilityLabel={label}>
      <View style={{ width: s[16], height: s[1.5], borderRadius: theme.radius.pill, backgroundColor: c.sunken, overflow: "hidden" }}>
        <View style={{ width: `${Math.round(fill * 100)}%`, height: "100%", borderRadius: theme.radius.pill, backgroundColor: c.primary }} />
      </View>
      <Text role="label" tabular>
        {label}
      </Text>
    </View>
  );
}

function docsFormatNumberFor(p: DocsProperty, v: number, stored: boolean): string {
  return stored ? docsFormatPropertyNumber(p, v, true) : docsFormatComputed(v, p);
}

function CheckMark({ on }: { on: boolean }) {
  const theme = useTheme();
  const Icon = on ? SquareCheck : Square;
  return (
    <View accessible accessibilityLabel={on ? "Checked" : "Not checked"}>
      <Icon size={20} color={on ? theme.colors.primaryStrong : theme.colors.textSubtle} strokeWidth={2} />
    </View>
  );
}

function PersonChip({ id, refs }: { id: string; refs: DocsRefs }) {
  const s = useTheme().space;
  const u = refs.users[id];
  if (!u) return null;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[1], maxWidth: "100%" }}>
      <Avatar name={u.name} src={u.avatar_url} size="xs" />
      <Text role="label" numberOfLines={1} style={{ flexShrink: 1 }}>
        {u.name}
      </Text>
    </View>
  );
}

function People({ ids, refs, compact }: { ids: string[]; refs: DocsRefs; compact: boolean }) {
  const known = ids.filter((id) => refs.users[id]);
  if (!known.length) return null;
  const shown = compact ? known.slice(0, 2) : known;
  return (
    <Wrap>
      {shown.map((id) => (
        <PersonChip key={id} id={id} refs={refs} />
      ))}
      {known.length > shown.length ? (
        <Text role="caption" tone="muted">
          {`+${known.length - shown.length}`}
        </Text>
      ) : null}
    </Wrap>
  );
}

/** Related rows: titles, "No access" for rows the viewer can't open, gone rows hidden. */
export function RelatedRows({ ids, refs, onOpenPage, compact = false }: { ids: string[]; refs: DocsRefs; onOpenPage?: ((id: string) => void) | undefined; compact?: boolean }) {
  const theme = useTheme();
  const known = ids.filter((id) => refs.pages[id]);
  if (!known.length) return null;
  const shown = compact ? known.slice(0, 3) : known;
  return (
    <Wrap>
      {shown.map((id) => {
        const ref = refs.pages[id]!;
        if (ref.title === null) {
          return (
            <Pill key={id} muted>
              <Lock size={12} color={theme.colors.textMuted} />
              <Text role="label" tone="muted">
                No access
              </Text>
            </Pill>
          );
        }
        const name = `${ref.icon ? `${ref.icon} ` : ""}${docsTitle(ref.title)}`;
        return (
          <Pill key={id} onPress={onOpenPage ? () => onOpenPage(id) : undefined} label={`Open ${docsTitle(ref.title)}`}>
            <Text role="label" numberOfLines={1} style={{ flexShrink: 1, textDecorationLine: ref.deleted ? "line-through" : "none" }}>
              {name}
            </Text>
          </Pill>
        );
      })}
      {known.length > shown.length ? (
        <Text role="caption" tone="muted">
          {`+${known.length - shown.length}`}
        </Text>
      ) : null}
    </Wrap>
  );
}

function Files({ files, compact }: { files: DocsFileValue[]; compact: boolean }) {
  const theme = useTheme();
  const shown = compact ? files.slice(0, 2) : files;
  return (
    <Wrap>
      {shown.map((f, i) => (
        <Pill key={`${f.file_id ?? f.url ?? ""}${i}`}>
          {f.url ? <Link2 size={14} color={theme.colors.textMuted} /> : <FileIcon name={f.name} mime={f.mime ?? null} size={14} />}
          <Text role="label" numberOfLines={1} style={{ flexShrink: 1 }}>
            {f.name}
          </Text>
        </Pill>
      ))}
      {files.length > shown.length ? (
        <Text role="caption" tone="muted">
          {`+${files.length - shown.length}`}
        </Text>
      ) : null}
    </Wrap>
  );
}

function isDate(v: unknown): v is DocsDateValue {
  return !!v && typeof v === "object" && !Array.isArray(v) && typeof (v as DocsDateValue).start === "string";
}

/** A formula or rollup result: chips for rows and people, a check for booleans, a meter or text for numbers. */
function Computed({ prop, value, refs, compact, onOpenPage }: { prop: DocsProperty; value: DocsComputedValue; refs: DocsRefs; compact: boolean; onOpenPage?: ((id: string) => void) | undefined }) {
  if (typeof value === "boolean") return <CheckMark on={value} />;
  if (typeof value === "number" && prop.number_display) return <NumberMeter prop={prop} value={value} stored={false} />;
  const list = Array.isArray(value) ? value : [value];
  const pages = list.filter((x): x is { page: string } => !!x && typeof x === "object" && !Array.isArray(x) && "page" in x).map((x) => x.page);
  const people = list.filter((x): x is { person: string } => !!x && typeof x === "object" && !Array.isArray(x) && "person" in x).map((x) => x.person);
  if (pages.length && pages.length === list.length) return <RelatedRows ids={pages} refs={refs} onOpenPage={onOpenPage} compact={compact} />;
  if (people.length && people.length === list.length) return <People ids={people} refs={refs} compact={compact} />;
  if (Array.isArray(value) && value.every((x) => typeof x === "boolean")) {
    return (
      <Wrap>
        {value.map((x, i) => (
          <CheckMark key={i} on={x as boolean} />
        ))}
      </Wrap>
    );
  }
  const text = docsFormatComputed(value, prop, refs);
  if (!text) return null;
  return (
    <Text role="label" numberOfLines={compact ? 2 : undefined} tabular={typeof value === "number"}>
      {text}
    </Text>
  );
}

export interface ValueViewProps {
  prop: DocsProperty;
  value: DocsValue | undefined;
  refs: DocsRefs;
  /** Created / edited time and people come from the row. */
  row?: DocsRowMeta | undefined;
  /** Cells and cards: fewer lines, nothing for empty values. */
  compact?: boolean;
  /** Related rows open (not in cells, where a tap edits). */
  onOpenPage?: ((id: string) => void) | undefined;
  /** Button properties. */
  onPressButton?: (() => void) | undefined;
  pressing?: boolean;
}

function isEmpty(v: DocsValue | undefined): boolean {
  return v === null || v === undefined || v === "" || (Array.isArray(v) && v.length === 0);
}

/** A row value as its property shows it: every property type. */
export function ValueView({ prop, value, refs, row, compact = false, onOpenPage, onPressButton, pressing }: ValueViewProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const empty = compact ? null : (
    <Text role="label" tone="subtle">
      Empty
    </Text>
  );
  switch (prop.type) {
    case "button": {
      const label = prop.button?.label || prop.name;
      return (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={label}
          accessibilityState={{ disabled: !onPressButton || !!pressing, busy: !!pressing }}
          disabled={!onPressButton || pressing}
          onPress={onPressButton}
          hitSlop={s[1]}
          style={({ pressed }) => ({
            flexDirection: "row",
            alignItems: "center",
            gap: s[1],
            alignSelf: "flex-start",
            paddingHorizontal: s[3],
            minHeight: s[8],
            borderRadius: theme.radius.md,
            borderWidth: 1,
            borderColor: c.borderStrong,
            backgroundColor: pressed ? c.sunken : c.surface,
            opacity: onPressButton ? 1 : 0.6,
          })}
        >
          {pressing ? <Spinner size={14} /> : null}
          <Text role="label" weight={600} numberOfLines={1}>
            {label}
          </Text>
        </Pressable>
      );
    }
    case "created_time":
    case "last_edited_time": {
      const t = row ? docsFormatValue(prop, undefined, refs, undefined, row) : "";
      return t ? (
        <Text role="label" numberOfLines={1}>
          {t}
        </Text>
      ) : (
        empty
      );
    }
    case "created_by":
    case "last_edited_by": {
      const id = prop.type === "created_by" ? row?.created_by : row?.updated_by;
      return id && refs.users[id] ? <PersonChip id={id} refs={refs} /> : empty;
    }
    case "checkbox":
      return <CheckMark on={value === true} />;
  }
  if (isEmpty(value)) return empty;
  switch (prop.type) {
    case "select":
    case "status": {
      const o = docsOption(prop, value);
      return o ? <OptionChip name={o.name} color={o.color} dot={prop.type === "status"} /> : empty;
    }
    case "multi_select": {
      const opts = (Array.isArray(value) ? (value as string[]) : []).map((id) => docsOption(prop, id)).filter((o) => !!o);
      if (!opts.length) return empty;
      const shown = compact ? opts.slice(0, 3) : opts;
      return (
        <Wrap>
          {shown.map((o) => (
            <OptionChip key={o.id} name={o.name} color={o.color} />
          ))}
          {opts.length > shown.length ? (
            <Text role="caption" tone="muted">
              {`+${opts.length - shown.length}`}
            </Text>
          ) : null}
        </Wrap>
      );
    }
    case "person": {
      const ids = Array.isArray(value) ? (value as string[]) : [];
      return ids.some((id) => refs.users[id]) ? <People ids={ids} refs={refs} compact={compact} /> : empty;
    }
    case "relation":
      return <RelatedRows ids={Array.isArray(value) ? (value as string[]) : []} refs={refs} onOpenPage={onOpenPage} compact={compact} />;
    case "files":
      return <Files files={Array.isArray(value) ? (value as DocsFileValue[]) : []} compact={compact} />;
    case "number":
      if (typeof value !== "number") return empty;
      if (prop.number_display) return <NumberMeter prop={prop} value={value} stored />;
      return (
        <Text role="label" tabular numberOfLines={1}>
          {docsFormatPropertyNumber(prop, value, true)}
        </Text>
      );
    case "unique_id":
      return (
        <Text role="label" tabular numberOfLines={1}>
          {docsFormatUniqueID(prop, value)}
        </Text>
      );
    case "date": {
      if (!isDate(value)) return empty;
      return (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
          <Text role="label" numberOfLines={compact ? 2 : undefined} style={{ flexShrink: 1 }}>
            {docsFormatValue(prop, value, refs)}
          </Text>
          {value.remind ? <Bell size={12} color={c.textMuted} accessibilityLabel="Reminder set" /> : null}
        </View>
      );
    }
    case "formula":
    case "rollup":
      return <Computed prop={prop} value={value as DocsComputedValue} refs={refs} compact={compact} onOpenPage={onOpenPage} />;
    case "url":
      return (
        <Text role="label" tone="primary" numberOfLines={1} style={{ textDecorationLine: "underline" }}>
          {String(value).replace(/^https?:\/\//, "")}
        </Text>
      );
  }
  return (
    <Text role="label" numberOfLines={compact ? 2 : undefined}>
      {docsFormatValue(prop, value, refs)}
    </Text>
  );
}

/** What a screen reader hears for a cell: the property and its value as text. */
export function valueLabel(prop: DocsProperty, value: DocsValue | undefined, refs: DocsRefs, row?: DocsRowMeta & { title?: string }): string {
  const t = docsFormatValue(prop, value, refs, row?.title, row);
  return `${prop.name}: ${t || "empty"}`;
}
