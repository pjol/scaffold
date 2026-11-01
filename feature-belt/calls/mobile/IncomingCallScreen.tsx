import { errorMessage, useAcceptCallsRing, useCallsRings, useDeclineCallsRing } from "@scaffoldapp/shared";
import { Avatar, IconButton, Text, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { BellOff, BellRing, Phone, PhoneOff } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Vibration, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

/** Full-screen incoming call: who, Accept or Decline, vibration you can silence. */
export function IncomingCallScreen() {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const { ring: ringId } = useLocalSearchParams<{ ring: string }>();
  const rings = useCallsRings();
  const accept = useAcceptCallsRing();
  const decline = useDeclineCallsRing();
  const [silent, setSilent] = useState(false);
  const ring = rings.data?.items.find((r) => r.id === ringId && r.status === "ringing");

  // The ring ended elsewhere (answered on another device, missed, hung up): close.
  useEffect(() => {
    if (rings.data && !ring && !accept.isPending) router.back();
  }, [rings.data, ring, accept.isPending]);

  useEffect(() => {
    if (silent || !ring) return undefined;
    Vibration.vibrate([0, 700, 1300], true);
    return () => Vibration.cancel();
  }, [silent, ring]);

  if (!ring) return <Stack.Screen options={{ headerShown: false }} />;
  const group = ring.others.length > 0;
  const what = ring.title || (group ? `Group call · ${ring.others.length + 2} people` : "Video call");

  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.canvas, paddingTop: insets.top + theme.space[12], paddingBottom: insets.bottom + theme.space[12], paddingHorizontal: theme.space[6], justifyContent: "space-between", alignItems: "center" }}>
      <Stack.Screen options={{ headerShown: false, gestureEnabled: false }} />
      <View style={{ alignItems: "center", gap: theme.space[4] }} accessibilityLiveRegion="assertive">
        <Avatar name={ring.caller.display_name} src={ring.caller.avatar_url} size="xl" />
        <Title align="center">{ring.caller.display_name}</Title>
        <Text tone="muted" align="center">
          {what}
        </Text>
        <IconButton label={silent ? "Vibrate again" : "Silence"} icon={silent ? BellRing : BellOff} variant="secondary" onPress={() => setSilent((s) => !s)} />
      </View>
      <View style={{ flexDirection: "row", gap: theme.space[16], alignItems: "center" }}>
        <View style={{ alignItems: "center", gap: theme.space[2] }}>
          <IconButton
            label="Decline"
            icon={PhoneOff}
            variant="danger"
            size="xl"
            loading={decline.isPending}
            onPress={() => {
              Vibration.cancel();
              decline.mutate(ring.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't decline.")) });
            }}
          />
          <Text role="label">Decline</Text>
        </View>
        <View style={{ alignItems: "center", gap: theme.space[2] }}>
          <IconButton
            label="Accept"
            icon={Phone}
            variant="primary"
            size="xl"
            style={{ backgroundColor: theme.colors.success }}
            onPress={() => {
              Vibration.cancel();
              accept.mutate(ring.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't answer. The call may have ended.")) });
              router.replace({ pathname: "/meet/[code]", params: { code: ring.code, autojoin: "1" } });
            }}
          />
          <Text role="label">Accept</Text>
        </View>
      </View>
    </View>
  );
}
