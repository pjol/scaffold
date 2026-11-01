import { CircleAlert } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Animated from "react-native-reanimated";
import { haptic } from "../lib/haptics";
import { enterUp, fadeOut } from "../lib/motion";
import { useTheme } from "../theme/ThemeProvider";
import { Button } from "./Button";
import { Checkbox } from "./Checkbox";
import { LegalDocument, type LegalDocumentData } from "./LegalDocument";
import { Sheet } from "./Sheet";
import { Text } from "./Text";

export interface ConsentFormValue {
  accepted: true;
  termsVersion: string;
  privacyVersion: string;
}

export interface ConsentFormProps {
  terms: LegalDocumentData;
  privacy: LegalDocumentData;
  /** Record acceptance. Throw to keep the form and show the error. */
  onSubmit: (value: ConsentFormValue) => void | Promise<unknown>;
  /** @default "Agree and continue" */
  submitLabel?: string;
  /** Map a thrown error to copy. @default the error's message */
  errorText?: (error: unknown) => string;
  style?: StyleProp<ViewStyle>;
}

/**
 * Terms + Privacy acceptance: one required checkbox whose document names open the full
 * text in a sheet, then the submit button, disabled until checked.
 */
export function ConsentForm({ terms, privacy, onSubmit, submitLabel = "Agree and continue", errorText, style }: ConsentFormProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [agreed, setAgreed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState<"terms" | "privacy" | null>(null);
  const doc = open === "terms" ? terms : open === "privacy" ? privacy : null;

  const submit = async () => {
    if (!agreed || busy) return;
    setError(null);
    setBusy(true);
    try {
      await onSubmit({ accepted: true, termsVersion: terms.version, privacyVersion: privacy.version });
    } catch (e) {
      haptic.error();
      setError(errorText ? errorText(e) : e instanceof Error && e.message ? e.message : "That didn't go through. Try again.");
    } finally {
      setBusy(false);
    }
  };

  const link = (label: string, which: "terms" | "privacy"): ReactNode => (
    <Text
      tone="primary"
      weight={600}
      accessibilityRole="link"
      onPress={() => {
        haptic.light();
        setOpen(which);
      }}
      style={{ textDecorationLine: "underline" }}
    >
      {label}
    </Text>
  );

  return (
    <View style={[{ gap: s[6] }, style]}>
      <Checkbox
        checked={agreed}
        onCheckedChange={setAgreed}
        disabled={busy}
        accessibilityLabel="I agree to the Terms of Service and Privacy Policy"
        label={
          <Text>
            I agree to the {link("Terms of Service", "terms")} and {link("Privacy Policy", "privacy")}
          </Text>
        }
      />
      {error ? (
        <Animated.View
          entering={enterUp(theme)}
          exiting={fadeOut(theme)}
          accessibilityRole="alert"
          accessibilityLiveRegion="assertive"
          style={{ flexDirection: "row", gap: s[2], alignItems: "flex-start", paddingHorizontal: s[3], paddingVertical: s[3], borderRadius: theme.radius.md, backgroundColor: c.dangerSoft }}
        >
          <CircleAlert size={20} color={c.dangerStrong} strokeWidth={2.25} />
          <Text role="label" weight={600} tone="danger" style={{ flex: 1 }}>
            {error}
          </Text>
        </Animated.View>
      ) : null}
      <Button fullWidth disabled={!agreed} loading={busy} onPress={() => void submit()}>
        {submitLabel}
      </Button>
      <Sheet open={open !== null} onClose={() => setOpen(null)} title={doc?.title ?? ""} snapPoints={["92%"]}>
        {doc ? <LegalDocument document={doc} showTitle={false} style={{ flex: 1 }} /> : null}
      </Sheet>
    </View>
  );
}
