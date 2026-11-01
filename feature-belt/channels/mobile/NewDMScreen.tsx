import { errorMessage, orgsNounA, useActiveOrg, useChannelsWorkspace, useMe, useOpenChannelsDM, type ChannelsPerson } from "@scaffoldapp/shared";
import { Button, EmptyState, Screen, toast } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Building2 } from "lucide-react-native";
import { useState } from "react";
import { PeoplePicker } from "./PeoplePicker";

/** channels/new-dm: pick one person (a DM) or several (a group DM), then open it. */
export function NewDMScreen() {
  const { org } = useActiveOrg();
  const me = useMe();
  const config = useChannelsWorkspace(org?.id).data?.config;
  const open = useOpenChannelsDM(org?.id ?? "");
  const [people, setPeople] = useState<ChannelsPerson[]>([]);
  if (!org) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "New message" }} />
        <EmptyState icon={Building2} title={`Join ${orgsNounA()} first`} />
      </Screen>
    );
  }
  const max = Math.max(1, (config?.max_group_dm ?? 9) - 1);
  const start = () =>
    open.mutate(
      people.map((p) => p.id),
      {
        onSuccess: (c) => router.replace(`/channels/${c.id}`),
        onError: (e) => toast.error(errorMessage(e, "Couldn't open the conversation.")),
      },
    );
  return (
    <Screen
      edges={["bottom"]}
      keyboard
      footer={
        <Button fullWidth disabled={people.length === 0} loading={open.isPending} onPress={start}>
          {people.length > 1 ? `Message ${people.length} people` : "Message"}
        </Button>
      }
    >
      <Stack.Screen options={{ title: "New message" }} />
      <PeoplePicker orgId={org.id} selected={people} onChange={setPeople} max={max} meId={me.data?.id} />
    </Screen>
  );
}
