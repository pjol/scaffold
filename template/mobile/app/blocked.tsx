import { errorMessage } from "@scaffoldapp/shared";
import { Button, EmptyState, Screen, toast, useTheme } from "@scaffoldapp/ui-native";
import { LogOut, ShieldAlert } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { SupportOptions } from "@/components/SupportOptions";
import { useSignOut } from "@/lib/session";

/** The API refuses this account (banned or deleted: `account_suspended` on /v1/me). Support and sign-out only. */
export default function BlockedScreen() {
  const theme = useTheme();
  const signOut = useSignOut();
  const [leaving, setLeaving] = useState(false);
  return (
    <Screen centered gap={theme.space[6]}>
      <EmptyState icon={ShieldAlert} title="Your account is suspended" compact />
      <SupportOptions />
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
