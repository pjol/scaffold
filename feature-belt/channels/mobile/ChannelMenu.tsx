import {
  CHANNELS_NOTIFY_OPTIONS,
  channelsDisplayName,
  channelsEffectiveNotify,
  channelsNameFrom,
  errorMessage,
  isChannelsDirect,
  useChannelPrefs,
  useChannelsWorkspace,
  useConvertChannelsGroupDM,
  useLeaveChannel,
  useMoveChannelToSection,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Field, Input, Sheet, toast } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Bell, BellOff, FolderInput, Info, Lock, LogOut, Star, StarOff, X } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import type { ResourceMenuAction } from "@/features/registry";

const NOTIFY_HINT: Record<string, string> = {
  all: "Every new message",
  mentions: "Only when someone mentions you",
  nothing: "No notifications from here",
};

/**
 * The channel's own ⋮ actions (Details, Notifications, Mute, Star, Leave) for
 * `ResourceMenu`, plus the leave confirmation to render beside it.
 */
export function useChannelMenuActions(c: ChannelsDetail | undefined, opts: { details?: boolean } = {}): { actions: ResourceMenuAction[]; element: ReactNode } {
  const prefs = useChannelPrefs(c?.id ?? "", c?.org_id);
  const leave = useLeaveChannel(c?.org_id);
  const ws = useChannelsWorkspace(c?.org_id);
  const move = useMoveChannelToSection(c?.org_id);
  const convert = useConvertChannelsGroupDM(c?.id ?? "", c?.org_id);
  const [confirm, setConfirm] = useState(false);
  const [converting, setConverting] = useState<string | null>(null);
  if (!c) return { actions: [], element: null };
  const direct = isChannelsDirect(c);
  const effective = channelsEffectiveNotify(c);
  const set = (body: Parameters<typeof prefs.mutate>[0]) => prefs.mutate(body, { onError: (e) => toast.error(errorMessage(e)) });
  const actions: ResourceMenuAction[] = [];
  if (opts.details !== false) actions.push({ id: "details", label: "Details", icon: Info, onSelect: () => router.push(`/channels/${c.id}/info`) });
  if (c.member) {
    actions.push(
      {
        id: "notify",
        label: "Notifications",
        icon: Bell,
        hint: CHANNELS_NOTIFY_OPTIONS.find((o) => o.value === c.notify)?.label ?? "Default",
        choices: CHANNELS_NOTIFY_OPTIONS.map((o) => ({
          id: o.value,
          label: o.value === "default" ? `Default (${effective === "all" ? "all messages" : "mentions"})` : o.label,
          checked: c.notify === o.value,
          ...(o.value !== "default" ? { hint: NOTIFY_HINT[o.value] } : {}),
          onSelect: () => set({ notify: o.value }),
        })),
      },
      { id: "mute", label: c.muted ? "Unmute" : "Mute", icon: c.muted ? Bell : BellOff, onSelect: () => set({ muted: !c.muted }) },
      { id: "star", label: c.starred ? "Remove star" : "Star", icon: c.starred ? StarOff : Star, onSelect: () => set({ starred: !c.starred }) },
      ...((ws.data?.sections.length ?? 0) > 0
        ? [
            {
              id: "section",
              label: "Move to section",
              icon: FolderInput,
              hint: ws.data?.sections.find((x) => x.id === c.section_id)?.name ?? "",
              choices: [
                { id: "none", label: direct ? "Direct messages" : "Channels", checked: !c.section_id, onSelect: () => move.mutate({ channelId: c.id, sectionId: null }) },
                ...(ws.data?.sections ?? []).map((x) => ({ id: x.id, label: x.name, checked: c.section_id === x.id, onSelect: () => move.mutate({ channelId: c.id, sectionId: x.id }) })),
              ],
            },
          ]
        : []),
      ...(c.kind === "group_dm" && !c.archived
        ? [{ id: "convert", label: "Make a private channel", icon: Lock, onSelect: () => setConverting(channelsNameFrom(c.people.map((p) => p.display_name.split(" ")[0] ?? "").join("-"))) }]
        : []),
      { id: "leave", label: direct ? "Close conversation" : "Leave channel", icon: direct ? X : LogOut, destructive: !direct, onSelect: () => (direct ? doLeave() : setConfirm(true)) },
    );
  }
  function doLeave() {
    if (!c) return;
    return leave.mutateAsync(c.id).then(
      () => {
        router.replace("/channels");
      },
      (e: unknown) => {
        toast.error(errorMessage(e));
      },
    );
  }
  const element = (
    <>
    <Sheet
      open={converting !== null}
      onClose={() => setConverting(null)}
      title="Make a private channel"
      info="Everyone here and the history move to the new channel. You'll be its admin."
      footer={
        <Button fullWidth disabled={!converting} loading={convert.isPending} onPress={() => converting && convert.mutate(converting, { onSuccess: () => setConverting(null), onError: (e) => toast.error(errorMessage(e)) })}>
          Make channel
        </Button>
      }
    >
      <Field label="Name">
        <Input value={converting ?? ""} onChangeText={(v) => setConverting(channelsNameFrom(v))} autoCapitalize="none" autoCorrect={false} maxLength={80} />
      </Field>
    </Sheet>
    <ConfirmDialog
      open={confirm}
      onClose={() => setConfirm(false)}
      title={`Leave ${channelsDisplayName(c)}?`}
      confirmLabel="Leave"
      tone="danger"
      onConfirm={() => doLeave()}
    >
      {c.kind === "private" ? "Someone in the channel has to add you back." : "You can join again from Browse channels."}
    </ConfirmDialog>
    </>
  );
  return { actions, element };
}
