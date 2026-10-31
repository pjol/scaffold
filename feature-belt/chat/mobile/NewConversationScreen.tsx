import { errorMessage, useAddMembers, useChatConversation, useCreateConversation, type ChatUser } from "@scaffoldapp/shared";
import { Banner, Button, Chip, Field, Input, Screen, SegmentedControl, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { X } from "lucide-react-native";
import { useState } from "react";
import { Platform, View } from "react-native";
import { HeaderClose } from "@/components/HeaderClose";
import { PeopleList } from "./PeopleList";

type Mode = "direct" | "group";

/** chat/new: pick a person (direct), name a group and pick people, or (`?add=<id>`) add people to a group. */
export function NewConversationScreen() {
  const { add } = useLocalSearchParams<{ add?: string }>();
  const theme = useTheme();
  const s = theme.space;
  const [mode, setMode] = useState<Mode>("direct");
  const [title, setTitle] = useState("");
  const [picked, setPicked] = useState<ChatUser[]>([]);
  const create = useCreateConversation();
  const addMembers = useAddMembers(add ?? "");
  const target = useChatConversation(add ?? null);
  const multiple = !!add || mode === "group";
  const toggle = (u: ChatUser) => setPicked((l) => (l.some((x) => x.id === u.id) ? l.filter((x) => x.id !== u.id) : [...l, u]));
  const error = create.error ?? addMembers.error;

  const open = (id: string) => router.replace(`/chat/${id}`);

  const footer = add ? (
    <Button
      fullWidth
      disabled={picked.length === 0}
      loading={addMembers.isPending}
      onPress={() =>
        addMembers.mutate(
          picked.map((u) => u.id),
          {
            onSuccess: () => {
              toast.success(picked.length > 1 ? `Added ${picked.length} people.` : "Added.");
              router.back();
            },
          },
        )
      }
    >
      {picked.length > 1 ? `Add ${picked.length} people` : "Add"}
    </Button>
  ) : mode === "group" ? (
    <Button
      fullWidth
      disabled={!title.trim() || picked.length === 0}
      loading={create.isPending}
      onPress={() => create.mutate({ kind: "group", title: title.trim(), member_ids: picked.map((u) => u.id) }, { onSuccess: (d) => open(d.id) })}
    >
      Create group
    </Button>
  ) : undefined;

  return (
    <Screen keyboard edges={["bottom"]} gap={s[4]} {...(footer ? { footer } : {})}>
      <Stack.Screen
        options={{
          title: add ? "Add people" : "New message",
          ...(Platform.OS === "ios" ? { presentation: "modal" as const } : { animation: "slide_from_bottom" as const }),
          headerRight: () => <HeaderClose />,
        }}
      />
      {error ? <Banner tone="danger" title={errorMessage(error)} /> : null}
      {!add ? (
        <SegmentedControl<Mode>
          accessibilityLabel="Conversation type"
          value={mode}
          onValueChange={(m) => {
            setMode(m);
            setPicked([]);
          }}
          options={[
            { value: "direct", label: "Person" },
            { value: "group", label: "Group" },
          ]}
        />
      ) : null}
      {mode === "group" && !add ? (
        <Field label="Group name">
          <Input value={title} onChangeText={setTitle} maxLength={80} placeholder="Weekend plans" />
        </Field>
      ) : null}
      {multiple && picked.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }} accessibilityLabel="Selected people">
          {picked.map((u) => (
            <Chip key={u.id} icon={X} onPress={() => toggle(u)} accessibilityLabel={`Remove ${u.display_name}`}>
              {u.display_name}
            </Chip>
          ))}
        </View>
      ) : null}
      <PeopleList
        multiple={multiple}
        selected={picked}
        exclude={add ? (target.data?.members.map((m) => m.user.id) ?? []) : []}
        onPress={(u) => (multiple ? toggle(u) : create.mutate({ kind: "direct", user_id: u.id }, { onSuccess: (d) => open(d.id) }))}
      />
    </Screen>
  );
}
