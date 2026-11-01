import type { TypeRole } from "@scaffoldapp/tokens";
import type { StyleProp, TextStyle } from "react-native";
import { formatCents, type CentsDisplay } from "../lib/format";
import { AnimatedNumber } from "./AnimatedNumber";
import type { TextTone } from "./Text";

export interface MoneyProps {
  /** Integer minor units. */
  cents: number;
  currency?: string;
  decimals?: CentsDisplay;
  /** Roll digits on change. @default false */
  animate?: boolean;
  signed?: boolean;
  role?: TypeRole;
  tone?: TextTone;
  style?: StyleProp<TextStyle>;
}

/** `128500` → `$1,285`; `1250` → `$12.50`. Tabular figures. */
export function Money({ cents, currency, decimals = "auto", animate = false, signed, role = "bodyLg", tone, style }: MoneyProps) {
  return (
    <AnimatedNumber
      value={cents}
      role={role}
      {...(tone ? { tone } : {})}
      animate={animate}
      style={style}
      format={(n) => formatCents(n, { cents: decimals, ...(currency ? { currency } : {}), ...(signed ? { signDisplay: "exceptZero" as const } : {}) })}
    />
  );
}
