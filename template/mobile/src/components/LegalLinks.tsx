import { Button, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { View } from "react-native";

/** "Terms of Service · Privacy Policy" (sign-in, consent). */
export function LegalLinks() {
  const s = useTheme().space;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", justifyContent: "center", alignItems: "center", gap: s[2] }}>
      <Button variant="ghost" size="md" onPress={() => router.push({ pathname: "/legal/[doc]", params: { doc: "terms" } })}>
        Terms of Service
      </Button>
      <Text tone="subtle" accessibilityElementsHidden importantForAccessibility="no">
        ·
      </Text>
      <Button variant="ghost" size="md" onPress={() => router.push({ pathname: "/legal/[doc]", params: { doc: "privacy" } })}>
        Privacy Policy
      </Button>
    </View>
  );
}
