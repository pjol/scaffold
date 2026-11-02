import { ScrollView, type StyleProp, type ViewStyle } from "react-native";
import { Chip } from "../components/Chip";
import { useTheme } from "../theme/ThemeProvider";
import { sourcePlatformName, sourceProductName, type SourceStatus } from "./marks";
import { ProviderIcon } from "./ProviderIcon";

/** One chip. `id` is "all", "app", a connection id, or `provider:<id>` when grouped by platform. */
export interface SourceFilterOption {
  id: string;
  label: string;
  provider?: string;
  product?: string | null;
  count?: number;
  status?: SourceStatus;
}

export interface SourceFilterProps {
  options: SourceFilterOption[];
  value: string;
  onChange: (id: string) => void;
  /** Accessible name of the group. @default "Source" */
  label?: string;
  style?: StyleProp<ViewStyle>;
}

/** Source chips (All · <App> · each connected source) in a sideways-scrolling row. Hidden without a connected source. */
export function SourceFilter({ options, value, onChange, label = "Source", style }: SourceFilterProps) {
  const theme = useTheme();
  const s = theme.space;
  if (!options.some((o) => o.provider)) return null;
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      accessibilityLabel={label}
      contentContainerStyle={{ gap: s[2], paddingHorizontal: s[4], paddingVertical: s[1] }}
      style={[{ flexGrow: 0 }, style]}
    >
      {options.map((o) => {
        const note = o.status === "needs_reauth" ? ", needs reconnecting" : o.status === "blocked" ? ", blocked by your workspace" : "";
        return (
          <Chip
            key={o.id}
            size="sm"
            selected={o.id === value}
            accessibilityLabel={`${o.label}${note}`}
            onPress={() => onChange(o.id)}
            {...(o.provider ? { icon: <ProviderIcon provider={o.provider} product={o.product} size={16} /> } : {})}
            {...(o.count !== undefined ? { count: o.count } : {})}
          >
            {o.label}
          </Chip>
        );
      })}
    </ScrollView>
  );
}

/** A feature source as `GET /v1/<feature>/sources` returns it (shared `FeatureSource`). */
export interface SourceFilterSource {
  id: string;
  type: string;
  provider?: string;
  product?: string;
  account?: string;
  label: string;
  status?: SourceStatus;
}

export interface SourceFilterOptionsInput {
  appLabel: string;
  sources: SourceFilterSource[] | undefined;
  counts?: Record<string, number>;
  groupBy?: "connection" | "provider";
  allLabel?: string;
}

/** Builds SourceFilter options: All · <App> · connected sources (or one chip per platform). Same as ui-web's. */
export function sourceFilterOptions({ appLabel, sources, counts, groupBy = "connection", allLabel = "All" }: SourceFilterOptionsInput): SourceFilterOption[] {
  const withCount = (o: SourceFilterOption): SourceFilterOption => (counts && counts[o.id] !== undefined ? { ...o, count: counts[o.id] } : o);
  const out: SourceFilterOption[] = [withCount({ id: "all", label: allLabel }), withCount({ id: "app", label: appLabel })];
  const connected = (sources ?? []).filter((x) => x.type === "connected" && x.provider);
  if (groupBy === "provider") {
    const seen = new Set<string>();
    for (const x of connected) {
      if (seen.has(x.provider!)) continue;
      seen.add(x.provider!);
      out.push(withCount({ id: `provider:${x.provider}`, label: sourcePlatformName(x.provider!), provider: x.provider! }));
    }
    return out;
  }
  const names = connected.map((x) => x.label || sourceProductName(x.provider!, x.product));
  for (const [i, x] of connected.entries()) {
    const name = names[i]!;
    const ambiguous = names.filter((n) => n === name).length > 1;
    out.push(
      withCount({
        id: x.id,
        label: ambiguous && x.account ? `${name} · ${x.account}` : name,
        provider: x.provider!,
        ...(x.product ? { product: x.product } : {}),
        ...(x.status ? { status: x.status } : {}),
      }),
    );
  }
  return out;
}
