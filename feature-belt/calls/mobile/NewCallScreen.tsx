import { errorMessage, orgsNounA, useActiveOrg, useCallsPeople, useCreateCallsMeeting, useDebouncedValue, useStartCallsCall, type CallsUser } from "@scaffoldapp/shared";
import { Avatar, Button, Checkbox, EmptyState, ErrorState, Input, Screen, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Phone, Search, UserRoundSearch } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";

/** Start call: ring the people picked, or with nobody picked, start alone and share the link. */
export function NewCallScreen() {
  const theme = useTheme();
  const { org } = useActiveOrg();
  const [query, setQuery] = useState("");
  const q = useDebouncedValue(query.trim(), 250);
  const people = useCallsPeople(q.length === 1 ? "" : q);
  const start = useStartCallsCall();
  const create = useCreateCallsMeeting();
  const [chosen, setChosen] = useState<CallsUser[]>([]);
  const ids = new Set(chosen.map((u) => u.id));
  const toggle = (u: CallsUser) => setChosen((c) => (ids.has(u.id) ? c.filter((x) => x.id !== u.id) : c.length >= 16 ? c : [...c, u]));

  const call = () => {
    const handlers = {
      onSuccess: (m: { code: string }) => router.replace({ pathname: "/meet/[code]", params: { code: m.code, autojoin: "1" } }),
      onError: (e: unknown) => toast.error(errorMessage(e, "Couldn't start the call. Try again.")),
    };
    if (chosen.length === 0) create.mutate({ access: "open", org_id: org?.id ?? null }, handlers);
    else start.mutate({ user_ids: chosen.map((u) => u.id), org_id: org?.id ?? null }, handlers);
  };

  return (
    <Screen
      edges={["bottom"]}
      scroll
      keyboard
      footer={
        <Button fullWidth leftIcon={Phone} loading={start.isPending || create.isPending} onPress={call}>
          {chosen.length === 0 ? "Start call" : chosen.length === 1 ? `Call ${chosen[0]?.display_name ?? ""}` : `Call ${chosen.length} people`}
        </Button>
      }
    >
      <Stack.Screen options={{ title: "Start call" }} />
      <Input value={query} onChangeText={setQuery} placeholder="Name or email" accessibilityLabel="Search people" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} />
      {people.isPending ? (
        <View style={{ gap: theme.space[2] }}>
          {Array.from({ length: 4 }, (_, i) => (
            <SkeletonRow key={i} />
          ))}
        </View>
      ) : people.isError ? (
        <ErrorState title="Couldn't load people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
      ) : people.data.items.length === 0 ? (
        <EmptyState icon={UserRoundSearch} title={q ? "No one found" : "No one to call yet"} info={`You can call people who share ${orgsNounA()} with you.`} />
      ) : (
        <View>
          {people.data.items.map((u) => (
            <Pressable
              key={u.id}
              onPress={() => toggle(u)}
              accessibilityRole="checkbox"
              accessibilityState={{ checked: ids.has(u.id) }}
              accessibilityLabel={u.display_name}
              style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: theme.space[3], minHeight: theme.tap.xl, backgroundColor: pressed ? theme.colors.sunken : "transparent" })}
            >
              <Checkbox checked={ids.has(u.id)} onCheckedChange={() => toggle(u)} accessibilityLabel={u.display_name} />
              <Avatar name={u.display_name} src={u.avatar_url} size="sm" />
              <Text style={{ flex: 1 }} numberOfLines={1}>
                {u.display_name}
              </Text>
            </Pressable>
          ))}
        </View>
      )}
    </Screen>
  );
}
