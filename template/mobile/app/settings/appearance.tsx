import { getPalette, PALETTE_IDS } from "@scaffoldapp/tokens/registry";
import { Label, ListGroup, ListRow, Screen, SegmentedControl, useTheme, type ColorModePreference } from "@scaffoldapp/ui-native";
import { Check } from "lucide-react-native";
import { View } from "react-native";
import { usePreferences } from "@/lib/preferences";

/** System / Light / Dark, and the palette when the app ships more than one. Device-wide; kept across sign-outs. */
export default function AppearanceScreen() {
  const theme = useTheme();
  const s = theme.space;
  const prefs = usePreferences();
  return (
    <Screen edges={["bottom"]}>
      <View style={{ gap: s[3] }}>
        <Label tone="muted" accessibilityRole="header">
          Mode
        </Label>
        <SegmentedControl<ColorModePreference>
          accessibilityLabel="Mode"
          value={prefs.appearance}
          onValueChange={prefs.setAppearance}
          options={[
            { value: "system", label: "System" },
            { value: "light", label: "Light" },
            { value: "dark", label: "Dark" },
          ]}
        />
      </View>
      {PALETTE_IDS.length > 1 ? (
        <ListGroup title="Palette">
          {PALETTE_IDS.map((id) => {
            const palette = getPalette(id);
            const selected = prefs.palette === id;
            return (
              <ListRow
                key={id}
                title={palette.name}
                leading={<View style={{ width: s[6], height: s[6], borderRadius: s[3], backgroundColor: palette.colors[theme.mode].primary }} />}
                trailing={selected ? <Check size={20} color={theme.colors.primaryStrong} strokeWidth={2.5} /> : undefined}
                chevron={false}
                accessibilityHint={selected ? "Selected" : "Uses this palette"}
                onPress={() => prefs.setPalette(id)}
              />
            );
          })}
        </ListGroup>
      ) : null}
    </Screen>
  );
}
