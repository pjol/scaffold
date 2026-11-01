"use client";

import {
  activeCustomStatus,
  CHANNELS_NOTIFY_OPTIONS,
  channelsDisplayName,
  channelsFileSize,
  channelsFullTime,
  channelsMessagePath,
  channelsNameFrom,
  channelsPlainText,
  channelsShortTime,
  errorMessage,
  orgsNoun,
  presenceLabel,
  presenceStatusText,
  useAddChannelMembers,
  useArchiveChannel,
  useChannelPrefs,
  useChannelsFiles,
  useChannelsMembers,
  useChannelsPins,
  useDebouncedValue,
  useLeaveChannel,
  useOpenChannelsDM,
  useRemoveChannelMember,
  useUpdateChannel,
  useUserPresence,
  type ChannelsDetail,
  type ChannelsMember,
  type ChannelsNotify,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, Select, Sheet, SkeletonRow, Switch, Tab, TabPanel, Tabs, TabsList, TextArea, toast } from "@scaffoldapp/ui-web";
import { FileText, MessageSquare, MoreHorizontal, Pin, Search, UserMinus, UserPlus, Users } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PresenceAvatar } from "@/features/presence";
import { AddPeopleDialog } from "./Dialogs";

export type DetailsTab = "about" | "members" | "pinned" | "files";

function EditableField({ label, value, multiline, max, disabled, onSave }: { label: string; value: string; multiline?: boolean; max: number; disabled: boolean; onSave: (v: string) => Promise<unknown> }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [busy, setBusy] = useState(false);
  useEffect(() => setDraft(value), [value]);
  if (!editing) {
    return (
      <div className="flex items-start gap-3 rounded-lg border border-border p-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="type-label font-semibold text-fg">{label}</span>
          <span className="whitespace-pre-wrap break-words type-body text-fg-muted">{value || "Not set"}</span>
        </div>
        {!disabled && (
          <Button size="sm" variant="ghost" onClick={() => setEditing(true)}>
            Edit
          </Button>
        )}
      </div>
    );
  }
  const save = async () => {
    setBusy(true);
    try {
      await onSave(draft.trim());
      setEditing(false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border p-4">
      <Field label={label}>
        {multiline ? (
          <TextArea autoFocus value={draft} maxLength={max} onChange={(e) => setDraft(e.target.value)} autoGrow minRows={3} maxRows={8} />
        ) : (
          <Input autoFocus value={draft} maxLength={max} onChange={(e) => setDraft(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void save()} />
        )}
      </Field>
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
          Cancel
        </Button>
        <Button size="sm" loading={busy} onClick={() => void save()}>
          Save
        </Button>
      </div>
    </div>
  );
}

function About({ channel, onClose }: { channel: ChannelsDetail; onClose: () => void }) {
  const router = useRouter();
  const update = useUpdateChannel(channel.id);
  const archive = useArchiveChannel(channel.id);
  const prefs = useChannelPrefs(channel.id, channel.org_id);
  const leave = useLeaveChannel(channel.org_id);
  const [confirm, setConfirm] = useState<"leave" | "archive" | "private" | null>(null);
  const direct = channel.kind === "dm" || channel.kind === "group_dm";
  const p = channel.permissions;
  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-3">
        {!direct && (
          <EditableField
            label="Name"
            value={`#${channel.name}`}
            max={81}
            disabled={!p.edit || channel.archived}
            onSave={(v) => update.mutateAsync({ name: channelsNameFrom(v) })}
          />
        )}
        <EditableField label="Topic" value={channel.topic} max={250} disabled={!channel.member || channel.archived} onSave={(v) => update.mutateAsync({ topic: v })} />
        {!direct && <EditableField label="Description" value={channel.description} multiline max={1000} disabled={!p.edit || channel.archived} onSave={(v) => update.mutateAsync({ description: v })} />}
        {channel.created_by && (
          <p className="type-label text-fg-muted">
            Created by {channel.created_by.display_name} on {channelsFullTime(channel.created_at)}
          </p>
        )}
      </div>

      {channel.member && (
        <section className="flex flex-col gap-3">
          <h3 className="type-title-sm text-fg">Notifications</h3>
          <Field label="Notify me about" info="Default is every message in direct messages and mentions in channels. Do Not Disturb pauses all of them.">
            <Select
              value={channel.notify}
              onValueChange={(v) => prefs.mutate({ notify: v as ChannelsNotify }, { onError: (e) => toast.error(errorMessage(e)) })}
              options={CHANNELS_NOTIFY_OPTIONS.map((o) => ({ value: o.value, label: o.label }))}
            />
          </Field>
          <Switch checked={channel.muted} onCheckedChange={(v) => prefs.mutate({ muted: v }, { onError: (e) => toast.error(errorMessage(e)) })} label="Mute" info="No notifications and no bold in the sidebar. Mentions still show a badge." />
        </section>
      )}

      {!direct && (p.set_default || p.edit || p.archive || channel.member) && (
        <section className="flex flex-col gap-3">
          <h3 className="type-title-sm text-fg">Settings</h3>
          {p.set_default && channel.kind === "public" && !channel.archived && (
            <Switch
              checked={channel.is_default}
              onCheckedChange={(v) => update.mutate({ is_default: v }, { onError: (e) => toast.error(errorMessage(e)) })}
              label="Everyone joins it"
              info={`Every member of the ${orgsNoun()} is added once, now and when they join. They can leave.`}
            />
          )}
          <div className="flex flex-wrap gap-2">
            {p.edit && channel.kind === "public" && !channel.is_default && !channel.archived && (
              <Button variant="secondary" size="sm" onClick={() => setConfirm("private")}>
                Make private
              </Button>
            )}
            {p.archive && (
              <Button variant={channel.archived ? "secondary" : "danger-ghost"} size="sm" onClick={() => (channel.archived ? archive.mutate(false, { onError: (e) => toast.error(errorMessage(e)) }) : setConfirm("archive"))}>
                {channel.archived ? "Unarchive channel" : "Archive channel"}
              </Button>
            )}
            {channel.member && (
              <Button variant="danger-ghost" size="sm" onClick={() => setConfirm("leave")}>
                Leave channel
              </Button>
            )}
          </div>
        </section>
      )}

      <ConfirmDialog
        open={confirm === "leave"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Leave ${channelsDisplayName(channel)}?`}
        confirmLabel="Leave"
        tone="danger"
        onConfirm={async () => {
          await leave.mutateAsync(channel.id);
          onClose();
          router.push("/channels");
        }}
      >
        {channel.kind === "private" ? "You need an invitation to come back." : "You can join again from Browse channels."}
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "archive"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Archive ${channelsDisplayName(channel)}?`}
        confirmLabel="Archive"
        tone="danger"
        onConfirm={() => archive.mutateAsync(true)}
      >
        Nobody can post in it. Its messages stay searchable and it can be unarchived.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "private"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Make ${channelsDisplayName(channel)} private?`}
        confirmLabel="Make private"
        onConfirm={() => update.mutateAsync({ kind: "private" })}
      >
        Only current members keep access. This can't be undone.
      </ConfirmDialog>
    </div>
  );
}

function MemberRow({ member: m, channel, meId, onRemove }: { member: ChannelsMember; channel: ChannelsDetail; meId: string | undefined; onRemove: (m: ChannelsMember) => void }) {
  const router = useRouter();
  const presence = useUserPresence(m.user.deleted ? null : m.user.id);
  const status = activeCustomStatus(presence);
  const openDM = useOpenChannelsDM(channel.org_id);
  const me = m.user.id === meId;
  return (
    <li className="flex min-h-tap-xl items-center gap-3 rounded-md px-2 py-2">
      <PresenceAvatar userId={m.user.deleted ? null : m.user.id} name={m.user.display_name} src={m.user.avatar_url} size="md" showOffline />
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className="shrink-0 truncate type-body text-fg">
          {m.user.display_name}
          {me ? " (you)" : ""}
        </span>
        {m.role === "admin" && <Badge size="sm">Channel admin</Badge>}
        <span className="min-w-0 truncate type-caption text-fg-muted">{status ? presenceStatusText(status) : presenceLabel(presence)}</span>
      </span>
      {!me && !m.user.deleted && (
        <Menu trigger={<IconButton size="sm" variant="ghost" label={`Actions for ${m.user.display_name}`} icon={<MoreHorizontal />} />}>
          <MenuItem
            icon={<MessageSquare />}
            onSelect={() => openDM.mutate([m.user.id], { onSuccess: (d) => router.push(`/channels/${d.id}`), onError: (e) => toast.error(errorMessage(e)) })}
          >
            Message
          </MenuItem>
          {channel.permissions.remove_members && channel.kind !== "dm" && channel.kind !== "group_dm" && (
            <MenuItem icon={<UserMinus />} destructive onSelect={() => onRemove(m)}>
              Remove from channel
            </MenuItem>
          )}
        </Menu>
      )}
    </li>
  );
}

function Members({ channel, meId }: { channel: ChannelsDetail; meId: string | undefined }) {
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q, 250);
  const members = useChannelsMembers(channel.id, query);
  const add = useAddChannelMembers(channel.id);
  const remove = useRemoveChannelMember(channel.id);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<ChannelsMember | null>(null);
  const direct = channel.kind === "dm" || channel.kind === "group_dm";
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find members" aria-label="Find members" leadingIcon={<Search />} wrapperClassName="flex-1" />
        {!direct && channel.permissions.post && (
          <Button variant="secondary" leftIcon={<UserPlus />} onClick={() => setAdding(true)}>
            Add people
          </Button>
        )}
      </div>
      {members.isPending ? (
        <div className="flex flex-col gap-1" aria-hidden>
          {[0, 1, 2, 3].map((i) => (
            <SkeletonRow key={i} />
          ))}
        </div>
      ) : members.isError ? (
        <ErrorState compact title="Couldn't load members" info={members.error.message} onRetry={() => void members.refetch()} retrying={members.isFetching} />
      ) : members.data.items.length === 0 ? (
        <EmptyState compact icon={<Users />} title={q ? "Nobody matches" : "No members"} />
      ) : (
        <>
          <ul className="flex flex-col" aria-label="Members">
            {members.data.items.map((m) => (
              <MemberRow key={m.user.id} member={m} channel={channel} meId={meId} onRemove={setRemoving} />
            ))}
          </ul>
          <LoadMore hasNextPage={members.hasNextPage} isFetchingNextPage={members.isFetchingNextPage} fetchNextPage={members.fetchNextPage} />
        </>
      )}
      <AddPeopleDialog orgId={channel.org_id} channelId={channel.id} channelName={channelsDisplayName(channel)} open={adding} onOpenChange={setAdding} onAdd={(ids) => add.mutateAsync(ids)} busy={add.isPending} />
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.user.display_name ?? "them"} from ${channelsDisplayName(channel)}?`}
        confirmLabel="Remove"
        tone="danger"
        onConfirm={() => (removing ? remove.mutateAsync(removing.user.id) : undefined)}
        errorText={(e) => errorMessage(e)}
      >
        {channel.kind === "private" ? "They lose access to its messages." : "They can join again."}
      </ConfirmDialog>
    </div>
  );
}

function Pinned({ channel, onJump }: { channel: ChannelsDetail; onJump: (path: string) => void }) {
  const pins = useChannelsPins(channel.id);
  if (pins.isPending) return <SkeletonRow />;
  if (pins.isError) return <ErrorState compact title="Couldn't load pinned messages" info={pins.error.message} onRetry={() => void pins.refetch()} retrying={pins.isFetching} />;
  if (pins.data.items.length === 0) return <EmptyState compact icon={<Pin />} title="Nothing pinned" />;
  return (
    <ul className="flex flex-col gap-2" aria-label="Pinned messages">
      {pins.data.items.map((m) => (
        <li key={m.id}>
          <button type="button" onClick={() => onJump(channelsMessagePath(m))} className="flex w-full flex-col gap-1 rounded-lg border border-border p-3 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
            <span className="flex items-baseline gap-2">
              <span className="truncate type-label font-semibold text-fg">{m.sender?.display_name ?? m.bot_name}</span>
              <span className="type-caption text-fg-muted">{channelsShortTime(m.created_at)}</span>
            </span>
            <span className="line-clamp-3 type-label text-fg">{m.title ?? channelsPlainText(m.body, m.mentions)}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function Files({ channel }: { channel: ChannelsDetail }) {
  const files = useChannelsFiles(channel.id);
  if (files.isPending) return <SkeletonRow />;
  if (files.isError) return <ErrorState compact title="Couldn't load files" info={files.error.message} onRetry={() => void files.refetch()} retrying={files.isFetching} />;
  if (files.data.items.length === 0) return <EmptyState compact icon={<FileText />} title="No files yet" />;
  return (
    <>
      <ul className="flex flex-col gap-1" aria-label="Files">
        {files.data.items.map((f) => (
          <li key={f.id}>
            <a href={f.url} target="_blank" rel="noopener noreferrer" className="flex min-h-tap-xl items-center gap-3 rounded-md px-2 py-2 transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
              {f.kind === "image" ? <img src={f.url} alt="" className="size-10 rounded-md object-cover" /> : <FileText aria-hidden className="size-6 text-fg-muted" />}
              <span className="flex min-w-0 flex-1 items-baseline gap-2">
                <span className="truncate type-body text-fg">{f.file_name || "File"}</span>
                <span className="shrink-0 type-caption text-fg-muted">{[channelsFileSize(f.size_bytes), f.sender?.display_name, channelsShortTime(f.created_at)].filter(Boolean).join(" · ")}</span>
              </span>
            </a>
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={files.hasNextPage} isFetchingNextPage={files.isFetchingNextPage} fetchNextPage={files.fetchNextPage} />
    </>
  );
}

/** Channel details: about and settings, members, pinned messages, files. */
export function ChannelDetails({ channel, meId, open, tab, onTabChange, onOpenChange }: { channel: ChannelsDetail; meId: string | undefined; open: boolean; tab: DetailsTab; onTabChange: (t: DetailsTab) => void; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={channelsDisplayName(channel)} size="lg">
      <Tabs value={tab} onValueChange={(v) => onTabChange(v as DetailsTab)}>
        <TabsList aria-label="Channel details">
          <Tab value="about">About</Tab>
          <Tab value="members" count={channel.member_count}>
            Members
          </Tab>
          <Tab value="pinned" count={channel.pinned_count || undefined}>
            Pinned
          </Tab>
          <Tab value="files">Files</Tab>
        </TabsList>
        <TabPanel value="about" className="pt-6">
          <About channel={channel} onClose={() => onOpenChange(false)} />
        </TabPanel>
        <TabPanel value="members" className="pt-6">
          <Members channel={channel} meId={meId} />
        </TabPanel>
        <TabPanel value="pinned" className="pt-6">
          <Pinned
            channel={channel}
            onJump={(path) => {
              onOpenChange(false);
              router.push(path);
            }}
          />
        </TabPanel>
        <TabPanel value="files" className="pt-6">
          <Files channel={channel} />
        </TabPanel>
      </Tabs>
    </Sheet>
  );
}
