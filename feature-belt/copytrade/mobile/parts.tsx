import { copytradeStatusLabel, formatCopytradePct, type CopytradePerson, type CopytradeStatus } from "@scaffoldapp/shared";
import { Avatar, Badge, InfoTip, Input, Stepper, Switch, Text, TitleSm, useTheme, type BadgeTone, type StepperProps } from "@scaffoldapp/ui-native";
import { ChevronRight } from "lucide-react-native";
import type { ReactNode } from "react";
import { Pressable, View } from "react-native";
import { signTone, Stat, StatGrid } from "@/features/exchange/parts";

/** A heading on the canvas and what it heads (heading→content 12). */
export function Group({ title, info, children }: { title: string; info?: ReactNode; children: ReactNode }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <TitleSm style={{ flexShrink: 1 }}>{title}</TitleSm>
        {info ? <InfoTip content={info} label={`About ${title}`} /> : null}
      </View>
      {children}
    </View>
  );
}

const statusTones: Record<CopytradeStatus, BadgeTone> = { active: "success", paused: "warning", stopping: "neutral", stopped: "neutral" };

export function CopyStatusBadge({ status }: { status: CopytradeStatus }) {
  return (
    <Badge size="sm" tone={statusTones[status]}>
      {copytradeStatusLabel(status)}
    </Badge>
  );
}

/** A percentage, green or red by sign ("+12.50%"); "—" when there's none yet. */
export function PctText({ pct, role = "label", signed = true }: { pct: string | null | undefined; role?: "caption" | "label"; signed?: boolean }) {
  return (
    <Text role={role} tabular tone={signed ? signTone(pct) : "default"}>
      {formatCopytradePct(pct, signed)}
    </Text>
  );
}

/** Avatar and name, then any badges. */
export function PersonTitle({ person, size = "sm", children }: { person: CopytradePerson; size?: "sm" | "md"; children?: ReactNode }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], flexShrink: 1, minWidth: 0 }}>
      <Avatar name={person.display_name} src={person.avatar_url} size={size} />
      <View style={{ flexShrink: 1, minWidth: 0, flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: s[2] }}>
        <Text role={size === "md" ? "titleSm" : "label"} weight={600} numberOfLines={2} style={{ flexShrink: 1 }}>
          {person.display_name}
        </Text>
        {children}
      </View>
    </View>
  );
}

/** One record: a title line with an action, then label/value pairs two a row. Pressable when `onPress` is set. */
export function Item({
  title,
  cells,
  action,
  onPress,
  accessibilityLabel,
}: {
  title: ReactNode;
  cells: [string, ReactNode][];
  action?: ReactNode;
  onPress?: () => void;
  accessibilityLabel?: string;
}) {
  const theme = useTheme();
  const s = theme.space;
  const body = (
    <>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base }}>
        <View style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>{title}</View>
        {action}
        {onPress && !action ? <ChevronRight size={20} color={theme.colors.textSubtle} strokeWidth={2.25} /> : null}
      </View>
      <StatGrid>
        {cells.map(([label, value]) => (
          <Stat key={label} label={label}>
            {value}
          </Stat>
        ))}
      </StatGrid>
    </>
  );
  const frame = { gap: s[2], paddingVertical: s[3], borderBottomWidth: 1, borderBottomColor: theme.colors.border };
  if (!onPress) return <View style={frame}>{body}</View>;
  return (
    <Pressable
      accessibilityRole="button"
      {...(accessibilityLabel ? { accessibilityLabel } : {})}
      onPress={onPress}
      style={({ pressed }) => [frame, { backgroundColor: pressed ? theme.colors.sunken : "transparent" }]}
    >
      {body}
    </Pressable>
  );
}

/** Small label/value lines (a sheet's summary). */
export function Facts({ rows }: { rows: [string, string][] }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[1.5] }}>
      {rows.map(([k, v]) => (
        <View key={k} style={{ flexDirection: "row", justifyContent: "space-between", gap: s[3] }}>
          <Text role="caption" tone="muted" style={{ flexShrink: 1 }}>
            {k}
          </Text>
          <Text role="caption" tabular>
            {v}
          </Text>
        </View>
      ))}
    </View>
  );
}

/** A labelled − / + stepper (the Stepper itself has no visible label). */
export function StepperRow({ label, info, ...rest }: { label: string; info?: ReactNode } & Omit<StepperProps, "label">) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexShrink: 1 }}>
        <Text role="label">{label}</Text>
        {info ? <InfoTip content={info} label={`About ${label}`} /> : null}
      </View>
      <Stepper label={label} {...rest} />
    </View>
  );
}

/** A percentage limit that can be switched off (stop loss, drawdown limit): the switch sits on the label row. */
export function PctLimitField({
  label,
  info,
  on,
  onToggle,
  value,
  onChange,
  error,
}: {
  label: string;
  info: ReactNode;
  on: boolean;
  onToggle: (on: boolean) => void;
  value: string;
  onChange: (v: string) => void;
  error?: string | undefined;
}) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[2] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.base }}>
        <Text role="label" style={{ flexShrink: 1 }}>
          {label}
        </Text>
        <InfoTip content={info} label={`About ${label}`} />
        <View style={{ marginLeft: "auto" }}>
          <Switch value={on} onValueChange={onToggle} accessibilityLabel={label} />
        </View>
      </View>
      {on ? (
        <View style={{ gap: s[1.5] }}>
          <Input
            accessibilityLabel={`${label}, percent`}
            keyboardType="decimal-pad"
            value={value}
            onChangeText={(t) => onChange(decimalInput(t))}
            placeholder="0"
            invalid={!!error}
            trailing={
              <Text role="caption" tone="muted">
                %
              </Text>
            }
          />
          {error ? (
            <Text role="label" tone="danger">
              {error}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

/** Keeps digits and the first dot ("1.2.3" → "1.23"). */
export function decimalInput(t: string): string {
  const [int = "", ...rest] = t.replace(/[^0-9.]/g, "").split(".");
  return rest.length > 0 ? `${int}.${rest.join("")}` : int;
}

/** A plain non-negative decimal ("12", "12.5"). */
export function isDecimal(t: string): boolean {
  return /^\d+(\.\d+)?$/.test(t);
}

/** "10.00" → "10", "12.50" → "12.5". */
export function plainNumber(v: string): string {
  return v.includes(".") ? v.replace(/0+$/, "").replace(/\.$/, "") : v;
}

/** A percentage between `lo` and `hi`, or the error to show. */
export function pctError(v: string, lo: number, hi: number): string | undefined {
  if (!isDecimal(v) || Number(v) < lo || Number(v) > hi) return `Use ${lo}–${hi}%.`;
  return undefined;
}

export function day(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

export function when(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
