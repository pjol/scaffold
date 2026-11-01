import { errorMessage, useAcceptConsents, useConsentGate } from "@scaffoldapp/shared";
import { Banner, Button, ConsentForm, InfoTip, Screen, Title, enterUp, toast, useTheme } from "@scaffoldapp/ui-native";
import { LogOut } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import Animated from "react-native-reanimated";
import { SupportOptions } from "@/components/SupportOptions";
import { useLegalDocument } from "@/lib/legal";
import { useSignOut } from "@/lib/session";
import { appName, openStore } from "@/lib/version";

/**
 * Consent gate: shown after sign-in, after a policy update and whenever the API answers
 * `consent_required`. Accepting updates `me`, which lifts the gate.
 */
export default function ConsentScreen() {
  const theme = useTheme();
  const s = theme.space;
  const gate = useConsentGate();
  const accept = useAcceptConsents();
  const signOut = useSignOut();
  const terms = useLegalDocument("terms");
  const privacy = useLegalDocument("privacy");
  const [leaving, setLeaving] = useState(false);

  return (
    <Screen>
      <Animated.View entering={enterUp(theme)} style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
        <Title>Terms and privacy</Title>
        <InfoTip size="md" content={`${appName()} needs your agreement to its Terms of Service and Privacy Policy before you continue. Tap either name to read it.`} />
      </Animated.View>
      {!gate.bundledVersionsMatch ? <Banner tone="warning" title="Update the app to read the latest terms." action={{ label: "Update", onPress: () => void openStore().catch(() => undefined) }} /> : null}
      <Animated.View entering={enterUp(theme, 1)}>
        <ConsentForm terms={terms} privacy={privacy} onSubmit={() => accept.mutateAsync()} errorText={(e) => errorMessage(e, "Couldn't save your agreement. Try again.")} />
      </Animated.View>
      <SupportOptions title="Questions" />
      <View style={{ alignItems: "center" }}>
        <Button
          variant="ghost"
          size="md"
          leftIcon={LogOut}
          loading={leaving}
          onPress={async () => {
            setLeaving(true);
            try {
              await signOut();
            } catch (e) {
              toast.error(errorMessage(e, "Couldn't sign out. Try again."));
              setLeaving(false);
            }
          }}
        >
          Sign out
        </Button>
      </View>
    </Screen>
  );
}
