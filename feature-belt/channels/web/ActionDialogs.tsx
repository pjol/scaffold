"use client";

import {
  activeCustomStatus,
  channelsDisplayName,
  channelsLocalInput,
  channelsLocalTime,
  channelsNameFrom,
  errorMessage,
  presenceLabel,
  presenceStatusText,
  useChannelsForward,
  useChannelsCreatePoll,
  useChannelsProfile,
  useChannelsWorkspace,
  useConvertChannelsGroupDM,
  useOpenChannelsDM,
  useUserPresence,
  type ChannelsCachedMessage,
  type ChannelsDetail,
  type ChannelsSidebarChannel,
} from "@scaffoldapp/shared";
import { Badge, Button, Dialog, EmptyState, ErrorState, Field, Input, Skeleton, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Clock, MessageSquare, Plus, Search, Trash2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { PresenceAvatar } from "@/features/presence";
import { ChannelIcon } from "./ChannelScreen";

/** Pick a date and time (local), for "Remind me…" and "Send later". */
export function PickTimeDialog({
  open,
  onOpenChange,
  title,
  confirmLabel,
  onPick,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  confirmLabel: string;
  onPick: (at: Date) => Promise<unknown> | void;
}) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (open) setValue(channelsLocalInput(new Date(Date.now() + 60 * 60_000)));
  }, [open]);
  const at = value ? new Date(value) : null;
  const valid = !!at && !Number.isNaN(at.getTime()) && at.getTime() > Date.now() + 30_000;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      size="sm"
      footer={
        <Button
          disabled={!valid}
          loading={busy}
          onClick={async () => {
            if (!at) return;
            setBusy(true);
            try {
              await onPick(at);
              onOpenChange(false);
            } finally {
              setBusy(false);
            }
          }}
        >
          {confirmLabel}
        </Button>
      }
    >
      <Field label="Date and time" error={value && !valid ? "Choose a time in the future." : undefined}>
        <Input type="datetime-local" value={value} min={channelsLocalInput(new Date())} onChange={(e) => setValue(e.target.value)} leadingIcon={<Clock />} />
      </Field>
    </Dialog>
  );
}

/** Forward a message to another conversation in the workspace (the server says where it may go). */
export function ForwardDialog({ message, orgId, onOpenChange }: { message: ChannelsCachedMessage | null; orgId: string; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  const ws = useChannelsWorkspace(orgId);
  const forward = useChannelsForward();
  const [q, setQ] = useState("");
  const [target, setTarget] = useState<ChannelsSidebarChannel | null>(null);
  const [comment, setComment] = useState("");
  useEffect(() => {
    if (message) {
      setQ("");
      setTarget(null);
      setComment("");
    }
  }, [message]);
  const list = useMemo(() => {
    const query = q.trim().toLowerCase();
    return (ws.data?.channels ?? []).filter((c) => !c.archived && (!query || c.name.toLowerCase().includes(query))).slice(0, 50);
  }, [ws.data, q]);
  return (
    <Dialog
      open={message !== null}
      onOpenChange={onOpenChange}
      title="Forward message"
      info="Messages from private channels and DMs can only go where everyone is already in that conversation."
      footer={
        <Button
          disabled={!target}
          loading={forward.isPending}
          onClick={() =>
            message &&
            target &&
            forward.mutate(
              { message, channel_id: target.id, comment: comment.trim() },
              {
                onSuccess: (m) => {
                  onOpenChange(false);
                  toast.success(`Forwarded to ${channelsDisplayName(target)}.`, { action: { label: "Open", onClick: () => router.push(`/channels/${m.channel_id}?message=${m.id}`) } });
                },
                onError: (e) => toast.error(errorMessage(e)),
              },
            )
          }
        >
          Forward
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        {target ? (
          <div className="flex items-center gap-2 rounded-md border border-border px-3 py-2">
            <ChannelIcon channel={target} className="size-4 text-fg-muted" />
            <span className="min-w-0 flex-1 truncate type-label text-fg">{channelsDisplayName(target)}</span>
            <Button size="sm" variant="ghost" leftIcon={<X />} onClick={() => setTarget(null)}>
              Change
            </Button>
          </div>
        ) : (
          <>
            <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search conversations" aria-label="Search conversations" leadingIcon={<Search />} />
            <ul className="flex max-h-64 flex-col gap-0.5 overflow-y-auto" aria-label="Conversations">
              {ws.isPending ? (
                <Skeleton className="h-10 w-full" />
              ) : list.length === 0 ? (
                <EmptyState compact icon={<MessageSquare />} title="No conversations match" />
              ) : (
                list.map((c) => (
                  <li key={c.id}>
                    <button
                      type="button"
                      onClick={() => setTarget(c)}
                      className="flex min-h-tap-sm w-full items-center gap-2 rounded-md px-3 py-1.5 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      <ChannelIcon channel={c} className="size-4 text-fg-muted" />
                      <span className="min-w-0 flex-1 truncate type-label text-fg">{channelsDisplayName(c)}</span>
                    </button>
                  </li>
                ))
              )}
            </ul>
          </>
        )}
        <Field label="Add a message">
          <TextArea value={comment} onChange={(e) => setComment(e.target.value)} minRows={2} maxRows={6} maxLength={4000} />
        </Field>
      </div>
    </Dialog>
  );
}

/** Create a poll: a question, 2–10 options, one or several choices, named or anonymous. */
export function PollDialog({ channel, threadRootId, initial, open, onOpenChange }: { channel: ChannelsDetail; threadRootId: string | null; initial: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const create = useChannelsCreatePoll(channel.id);
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState<string[]>(["", ""]);
  const [multiple, setMultiple] = useState(false);
  const [anonymous, setAnonymous] = useState(false);
  useEffect(() => {
    if (open) {
      setQuestion(initial);
      setOptions(["", ""]);
      setMultiple(false);
      setAnonymous(false);
    }
  }, [open, initial]);
  const filled = options.map((o) => o.trim()).filter(Boolean);
  const valid = question.trim().length > 0 && filled.length >= 2 && new Set(filled).size === filled.length;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New poll"
      footer={
        <Button
          disabled={!valid}
          loading={create.isPending}
          onClick={() =>
            create.mutate(
              { question: question.trim(), options: filled, multiple, anonymous, thread_root_id: threadRootId },
              { onSuccess: () => onOpenChange(false), onError: (e) => toast.error(errorMessage(e)) },
            )
          }
        >
          Post poll
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Question">
          <Input autoFocus value={question} onChange={(e) => setQuestion(e.target.value)} maxLength={300} />
        </Field>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 type-label font-semibold text-fg">Options</legend>
          {options.map((o, i) => (
            <div key={i} className="flex items-center gap-2">
              <Input value={o} onChange={(e) => setOptions((list) => list.map((x, j) => (j === i ? e.target.value : x)))} maxLength={200} aria-label={`Option ${i + 1}`} wrapperClassName="flex-1" />
              {options.length > 2 && (
                <Button size="sm" variant="ghost" aria-label={`Remove option ${i + 1}`} leftIcon={<Trash2 />} onClick={() => setOptions((list) => list.filter((_, j) => j !== i))}>
                  <span className="sr-only">Remove</span>
                </Button>
              )}
            </div>
          ))}
          {options.length < 10 && (
            <Button size="sm" variant="ghost" leftIcon={<Plus />} className="w-fit" onClick={() => setOptions((list) => [...list, ""])}>
              Add option
            </Button>
          )}
        </fieldset>
        <Switch checked={multiple} onCheckedChange={setMultiple} label="Allow several choices" />
        <Switch checked={anonymous} onCheckedChange={setAnonymous} label="Anonymous votes" info="Counts show; who voted doesn't." />
      </div>
    </Dialog>
  );
}

/** A person's card: name, title, local time, status, teams; message them. */
export function ProfileCardDialog({ orgId, userId, onOpenChange }: { orgId: string; userId: string | null; onOpenChange: (o: boolean) => void }) {
  const router = useRouter();
  const card = useChannelsProfile(orgId, userId);
  const presence = useUserPresence(userId);
  const status = activeCustomStatus(presence);
  const openDM = useOpenChannelsDM(orgId);
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!userId) return undefined;
    const t = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(t);
  }, [userId]);
  const local = card.data ? channelsLocalTime(card.data.timezone, now) : null;
  return (
    <Dialog open={userId !== null} onOpenChange={onOpenChange} title={card.data?.display_name ?? "Profile"} size="sm">
      {card.isPending ? (
        <div className="flex gap-3" aria-busy>
          <Skeleton shape="circle" width={64} height={64} />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-4 w-1/3" />
          </div>
        </div>
      ) : card.isError ? (
        <ErrorState compact title="Couldn't load this profile" info={card.error.message} onRetry={() => void card.refetch()} retrying={card.isFetching} />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-4">
            <PresenceAvatar userId={card.data.deleted ? null : card.data.id} name={card.data.display_name} src={card.data.avatar_url} size="lg" />
            <div className="flex min-w-0 flex-col gap-1">
              {card.data.title && <span className="type-body text-fg">{card.data.title}</span>}
              <span className="type-label text-fg-muted">{presenceLabel(presence)}</span>
              {card.data.org_role !== "member" && (
                <Badge size="sm" className="w-fit">
                  {card.data.org_role === "owner" ? "Owner" : "Admin"}
                </Badge>
              )}
            </div>
          </div>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
            {status && (
              <>
                <dt className="type-label text-fg-muted">Status</dt>
                <dd className="type-label text-fg">{presenceStatusText(status)}</dd>
              </>
            )}
            {local && (
              <>
                <dt className="type-label text-fg-muted">Local time</dt>
                <dd className="type-label text-fg">{local}</dd>
              </>
            )}
            {card.data.teams.length > 0 && (
              <>
                <dt className="type-label text-fg-muted">Teams</dt>
                <dd className="flex flex-wrap gap-1">
                  {card.data.teams.map((t) => (
                    <Badge key={t.id} size="sm">
                      @{t.handle}
                    </Badge>
                  ))}
                </dd>
              </>
            )}
          </dl>
          {!card.data.deleted && (
            <Button
              variant="secondary"
              leftIcon={<MessageSquare />}
              loading={openDM.isPending}
              onClick={() =>
                openDM.mutate([card.data.id], {
                  onSuccess: (d) => {
                    onOpenChange(false);
                    router.push(`/channels/${d.id}`);
                  },
                  onError: (e) => toast.error(errorMessage(e)),
                })
              }
            >
              Message
            </Button>
          )}
        </div>
      )}
    </Dialog>
  );
}

/** Turn a group DM into a private channel with the same people and history. */
export function ConvertDialog({ channel, open, onOpenChange }: { channel: ChannelsDetail; open: boolean; onOpenChange: (o: boolean) => void }) {
  const convert = useConvertChannelsGroupDM(channel.id, channel.org_id);
  const [name, setName] = useState("");
  useEffect(() => {
    if (open) setName(channelsNameFrom(channel.people.map((p) => p.display_name.split(" ")[0] ?? "").join("-")));
  }, [open, channel.people]);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Make a private channel"
      info="Everyone here and the history move to the new channel. You'll be its admin."
      footer={
        <Button disabled={!name} loading={convert.isPending} onClick={() => convert.mutate(name, { onSuccess: () => onOpenChange(false), onError: (e) => toast.error(errorMessage(e)) })}>
          Make channel
        </Button>
      }
    >
      <Field label="Name">
        <Input autoFocus value={name} onChange={(e) => setName(channelsNameFrom(e.target.value))} leadingIcon={<span className={cn("type-label text-fg-muted")}>#</span>} maxLength={80} />
      </Field>
    </Dialog>
  );
}
