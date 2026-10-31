"use client";

import { getPalette, type PaletteId } from "@scaffoldapp/tokens";
import { SegmentedControl, cn, motion, useColorMode, useMotionTokens, usePalette, type ThemePreference } from "@scaffoldapp/ui-web";
import { Check, Monitor, Moon, Sun } from "lucide-react";
import { Screen, Section } from "@/components/shell/PageHeader";

function PaletteOption({ id, selected, onSelect }: { id: PaletteId; selected: boolean; onSelect: () => void }) {
  const t = useMotionTokens();
  const { mode } = useColorMode();
  const p = getPalette(id);
  const c = p.colors[mode];
  return (
    <motion.button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      whileTap={{ scale: t.pressScale }}
      transition={t.spring.press}
      className={cn("flex min-h-tap items-center gap-3 rounded-lg border p-3 text-left transition-colors", selected ? "border-primary bg-primary-soft" : "border-border bg-surface hover:border-border-strong")}
    >
      {/* Swatches show the palette's own colours, read from tokens. */}
      <span className="flex shrink-0 overflow-hidden rounded-sm border border-border" aria-hidden>
        <span className="size-6" style={{ background: c.canvas }} />
        <span className="size-6" style={{ background: c.primary }} />
        <span className="size-6" style={{ background: c.accent }} />
      </span>
      <span className="min-w-0 flex-1 truncate type-body font-medium text-fg">{p.name}</span>
      {selected && <Check className="size-4 shrink-0 text-primary-strong" aria-hidden />}
    </motion.button>
  );
}

/** Settings → Appearance: colour mode (System / Light / Dark) and, when the app ships more than one, the palette. */
export function AppearanceScreen() {
  const { preference, setPreference } = useColorMode();
  const { id, ids, setPalette } = usePalette();
  return (
    <Screen title="Appearance">
      <Section title="Mode" info="System follows your device's light or dark setting.">
        <SegmentedControl<ThemePreference>
          aria-label="Mode"
          value={preference}
          onValueChange={setPreference}
          className="max-w-form"
          options={[
            { value: "system", label: "System", icon: <Monitor aria-hidden /> },
            { value: "light", label: "Light", icon: <Sun aria-hidden /> },
            { value: "dark", label: "Dark", icon: <Moon aria-hidden /> },
          ]}
        />
      </Section>
      {ids.length > 1 && (
        <Section title="Palette">
          <div role="radiogroup" aria-label="Palette" className="grid max-w-form gap-2 sm:grid-cols-2">
            {ids.map((pid) => (
              <PaletteOption key={pid} id={pid} selected={pid === id} onSelect={() => setPalette(pid)} />
            ))}
          </div>
        </Section>
      )}
    </Screen>
  );
}
