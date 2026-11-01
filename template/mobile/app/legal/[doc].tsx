import { EmptyState, LegalDocument, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { FileX } from "lucide-react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { HeaderClose } from "@/components/HeaderClose";
import { useLegalDocument } from "@/lib/legal";

/** Terms of Service / Privacy Policy as a modal; public (reachable from sign-in). */
export default function LegalScreen() {
  const { doc } = useLocalSearchParams<{ doc: string }>();
  const kind = doc === "privacy" || doc === "terms" ? doc : null;
  return kind ? <LegalBody kind={kind} /> : <EmptyState icon={FileX} title="Document not found" />;
}

function LegalBody({ kind }: { kind: "terms" | "privacy" }) {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const document = useLegalDocument(kind);
  return (
    <>
      <Stack.Screen options={{ title: document.title, headerRight: () => <HeaderClose /> }} />
      <LegalDocument
        document={document}
        showTitle={false}
        style={{ flex: 1, backgroundColor: theme.colors.canvas }}
        contentContainerStyle={{ paddingHorizontal: layout.gutter, paddingTop: theme.space[4], paddingBottom: insets.bottom + theme.space[8], width: "100%", maxWidth: layout.maxContent + layout.gutter * 2, alignSelf: "center" }}
      />
    </>
  );
}
