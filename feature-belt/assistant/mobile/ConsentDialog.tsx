import { errorMessage, useAcceptAssistantConsent, type AssistantProvider } from "@scaffoldapp/shared";
import { Body, Button, Dialog, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { View } from "react-native";

/**
 * Asks once, before the first message, to send the user's content to the named AI provider
 * (Apple 5.1.2(i)). Settings → Assistant turns it off.
 */
export function ConsentDialog({ provider, open, onClose, onAccepted }: { provider: AssistantProvider; open: boolean; onClose: () => void; onAccepted: () => void }) {
  const theme = useTheme();
  const accept = useAcceptAssistantConsent();
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Use the assistant?"
      footer={
        <View style={{ gap: theme.space[2] }}>
          <Button
            loading={accept.isPending}
            onPress={() =>
              accept.mutate(provider.name, {
                onSuccess: () => {
                  onClose();
                  onAccepted();
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't save your choice. Try again.")),
              })
            }
          >
            Allow
          </Button>
          <Button variant="ghost" onPress={onClose}>
            Not now
          </Button>
        </View>
      }
    >
      <View style={{ gap: theme.space[3] }}>
        <Body>{`To answer, the assistant sends your messages, and the app data it reads for you, to ${provider.label}, which writes the replies.`}</Body>
        <Body tone="muted">
          You can turn this off in Settings.{" "}
          <Body
            tone="primary"
            accessibilityRole="link"
            onPress={() => {
              onClose();
              router.push("/legal/privacy");
            }}
          >
            Privacy Policy
          </Body>
        </Body>
      </View>
    </Dialog>
  );
}
