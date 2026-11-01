"use client";

import {
  CHANNELS_NOTIFY_OPTIONS,
  activeCustomStatus,
  channelsDisplayName,
  errorMessage,
  presenceStatusText,
  useActiveOrg,
  useChannel,
  useChannelPrefs,
  useChannelsTopic,
  useChannelsWorkspace,
  useJoinChannel,
  useLeaveChannel,
  useMe,
  useStartChannelCall,
  useUserPresence,
  type ChannelsDetail,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Skeleton, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Bell, BellOff, Hash, Info, LayoutList, Link2, Lock, LogOut, Phone, ShieldCheck, Star, StarOff, Users, X } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { PresenceAvatar } from "@/features/presence";
import { ChannelDetails, type DetailsTab } from "./ChannelDetails";
import { ChatView } from "./ChatView";
import { ForumView } from "./ForumView";
import { ThreadPane } from "./ThreadPane";

export function ChannelIcon({ channel, className }: { channel: Pick<ChannelsDetail, "kind" | "layout">; className?: string }) {
  const cls = cn("shrink-0", className);
  if (channel.kind === "private") return <Lock aria-label="Private" className={cls} />;
  if (channel.layout === "forum") return <LayoutList aria-label="Forum" className={cls} />;
  if (channel.kind === "group_dm") return <Users aria-label="Group message" className={cls} />;
  return <Hash aria-hidden className={cls} />;
}

function ScreenSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col" aria-busy aria-label="Loading channel">
      <div className="flex min-h-topbar items-center gap-3 border-b border-border px-4">
        <Skeleton className="h-5 w-40" />
      </div>
      <div className="flex flex-1 flex-col justify-end gap-5 px-gutter py-4 lg:px-5">
        {[0, 1, 2].map((i) => (
          <div key={i} className="flex gap-3">
            <Skeleton shape="circle" width={32} height={32} />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-4 w-2/3" />
            </div>
          </div>
        ))}
      </div>
      <div className="px-gutter pb-3 lg:px-5">
        <Skeleton className="h-24 w-full rounded-lg" />
      </div>
    </div>
  );
}

/** A DM's header title: the person with presence and status, or the group. */
function DirectTitle({ channel }: { channel: ChannelsDetail }) {
  const peer = channel.kind === "dm" ? channel.people[0] : undefined;
  const presence = useUserPresence(peer && !peer.deleted ? peer.id : null);
  const status = activeCustomStatus(presence);
  return (
    <>
      {peer ? <PresenceAvatar userId={peer.deleted ? null : peer.id} name={peer.display_name} src={peer.avatar_url} size="xs" /> : <ChannelIcon channel={channel} className="size-5 text-fg-muted" />}
      <span className="min-w-0 truncate type-title-sm text-fg">{channel.name}</span>
      {status && <span className="hidden min-w-0 truncate type-label text-fg-muted sm:inline">{presenceStatusText(status)}</span>}
    </>
  );
}

/** /channels/<id>: header, timeline or forum, composer, and the thread pane (`?thread=`). */
export function ChannelScreen({ id }: { id: string }) {
  const params = useSearchParams();
  const thread = params?.get("thread") ?? null;
  const message = params?.get("message") ?? null;
  const me = useMe();
  const channel = useChannel(id);
  useChannelsTopic(channel.data ? id : null);
  const { org, setActive } = useActiveOrg();

  // A link into another org's channel switches the workspace to that org.
  const orgId = channel.data?.org_id;
  useEffect(() => {
    if (orgId && org && org.id !== orgId) setActive(orgId);
  }, [orgId, org, setActive]);

  if (channel.isPending) return <ScreenSkeleton />;
  if (channel.isError) {
    const notFound = channel.error.status === 404;
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        {notFound ? (
          <EmptyState
            icon={<Hash />}
            title="Channel not found"
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button asChild variant="secondary" size="auto">
                  <Link href="/channels">Back to channels</Link>
                </Button>
                {me.data?.admin_role && (
                  <Button asChild variant="ghost" size="auto" leftIcon={<ShieldCheck />}>
                    <Link href={`/admin/channels/${id}${message ? `?around=${message}` : ""}${thread ? `${message ? "&" : "?"}thread=${thread}` : ""}`}>Open staff view</Link>
                  </Button>
                )}
              </div>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this channel" info={channel.error.message} onRetry={() => void channel.refetch()} retrying={channel.isFetching} />
        )}
      </div>
    );
  }
  return <Channel key={id} channel={channel.data} thread={thread} message={message} meId={me.data?.id} suspended={me.data?.status === "suspended"} />;
}

function Channel({ channel: c, thread, message, meId, suspended }: { channel: ChannelsDetail; thread: string | null; message: string | null; meId: string | undefined; suspended: boolean }) {
  const router = useRouter();
  const ws = useChannelsWorkspace(c.org_id);
  const config = ws.data?.config;
  const prefs = useChannelPrefs(c.id, c.org_id);
  const join = useJoinChannel(c.id);
  const leave = useLeaveChannel(c.org_id);
  const call = useStartChannelCall(c.id);
  const [details, setDetails] = useState<DetailsTab | null>(null);
  const [leaving, setLeaving] = useState(false);
  const direct = c.kind === "dm" || c.kind === "group_dm";
  const title = channelsDisplayName(c);

  const openThread = useCallback(
    (rootId: string, messageId?: string) => router.push(`/channels/${c.id}?thread=${rootId}${messageId ? `&message=${messageId}` : ""}`),
    [router, c.id],
  );
  const closeThread = () => router.replace(`/channels/${c.id}`);

  const toast_ = (e: unknown) => toast.error(errorMessage(e));
  const menu: ResourceMenuAction[] = [
    { id: "details", label: direct ? "Conversation details" : "Channel details", icon: Info, onSelect: () => setDetails("about") },
    ...(c.member
      ? [
          {
            id: "notify",
            label: "Notifications",
            icon: Bell,
            hint: CHANNELS_NOTIFY_OPTIONS.find((o) => o.value === c.notify)?.label ?? "",
            choices: CHANNELS_NOTIFY_OPTIONS.map((o) => ({ id: o.value, label: o.label, checked: c.notify === o.value, onSelect: () => prefs.mutate({ notify: o.value }, { onError: toast_ }) })),
          },
          { id: "mute", label: c.muted ? "Unmute" : "Mute", icon: c.muted ? Bell : BellOff, onSelect: () => prefs.mutate({ muted: !c.muted }, { onError: toast_ }) },
          { id: "star", label: c.starred ? "Remove from starred" : "Star", icon: c.starred ? StarOff : Star, onSelect: () => prefs.mutate({ starred: !c.starred }, { onError: toast_ }) },
        ]
      : []),
    { id: "link", label: "Copy link", icon: Link2, onSelect: () => void navigator.clipboard?.writeText(`${window.location.origin}/channels/${c.id}`).then(() => toast.success("Link copied.")) },
    ...(c.member ? [{ id: "leave", label: direct ? "Close conversation" : "Leave channel", icon: direct ? X : LogOut, destructive: true, onSelect: () => (direct ? void leave.mutateAsync(c.id).then(() => router.push("/channels"), toast_) : setLeaving(true)) }] : []),
  ];

  const composerSlot = suspended ? (
    <Notice text="Your account is suspended. You can read but not send." />
  ) : c.archived ? (
    <Notice text={`${title} is archived. Nobody can post in it.`} />
  ) : !c.member ? (
    <Notice text={`You're viewing ${title}.`}>
      <Button size="sm" loading={join.isPending} onClick={() => join.mutate(undefined, { onSuccess: () => toast.success(`Joined ${title}.`), onError: toast_ })}>
        Join channel
      </Button>
    </Notice>
  ) : direct && c.people.length === 1 && c.people[0]?.deleted ? (
    <Notice text="This account was deleted." />
  ) : undefined;

  return (
    <div className="relative flex min-h-0 flex-1">
      <div className={cn("flex min-w-0 flex-1 flex-col", thread && "hidden xl:flex")}>
        <header className="flex min-h-topbar items-center gap-1 border-b border-border px-2 lg:px-4">
          <IconButton className="lg:hidden" variant="ghost" label="Back to channels" icon={<ArrowLeft />} onClick={() => router.push("/channels")} />
          <button
            type="button"
            onClick={() => setDetails("about")}
            className="flex min-h-tap-sm min-w-0 shrink items-center gap-2 rounded-md px-2 py-1 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
          >
            {direct ? (
              <DirectTitle channel={c} />
            ) : (
              <>
                <ChannelIcon channel={c} className="size-5 text-fg-muted" />
                <span className="min-w-0 truncate type-title-sm text-fg">{c.name}</span>
              </>
            )}
          </button>
          {c.topic && !direct && (
            <Tooltip content={c.topic}>
              <span tabIndex={0} className="hidden min-w-0 flex-1 truncate type-label text-fg-muted md:block">
                {c.topic}
              </span>
            </Tooltip>
          )}
          <span className="flex-1" />
          {c.muted && <BellOff aria-label="Muted" className="size-4 shrink-0 text-fg-subtle" />}
          {!direct && (
            <Button variant="ghost" size="sm" leftIcon={<Users />} onClick={() => setDetails("members")} aria-label={`${c.member_count} members`}>
              <span className="tabular-nums">{c.member_count}</span>
            </Button>
          )}
          {config?.calls_enabled && c.member && !c.archived && (
            <IconButton
              variant="ghost"
              label="Start call"
              tooltip
              icon={<Phone />}
              loading={call.isPending}
              onClick={() => call.mutate(undefined, { onSuccess: (r) => router.push(r.path), onError: toast_ })}
            />
          )}
          <ResourceMenu feature="channels" kind="channels.channel" id={c.id} orgId={c.org_id} title={title} actions={menu} label={direct ? "Conversation actions" : "Channel actions"} />
        </header>
        {!config ? (
          <ScreenSkeleton />
        ) : c.layout === "forum" ? (
          <ForumView channel={c} activeThread={thread} onOpenThread={(root) => openThread(root)} />
        ) : (
          <ChatView channel={c} config={config} meId={meId} around={thread ? null : message} onOpenThread={openThread} composerSlot={composerSlot} />
        )}
        {c.layout === "forum" && composerSlot}
      </div>
      {thread && config && (
        <aside aria-label="Thread" className="flex min-h-0 w-full flex-col border-border xl:w-96 xl:shrink-0 xl:border-l">
          <ThreadPane key={thread} rootId={thread} channel={c} config={config} meId={meId} around={message} onClose={closeThread} onOpenThread={openThread} />
        </aside>
      )}
      {details && <ChannelDetails channel={c} meId={meId} open tab={details} onTabChange={setDetails} onOpenChange={(o) => !o && setDetails(null)} />}
      <ConfirmDialog
        open={leaving}
        onOpenChange={setLeaving}
        title={`Leave ${title}?`}
        confirmLabel="Leave"
        tone="danger"
        onConfirm={async () => {
          await leave.mutateAsync(c.id);
          router.push("/channels");
        }}
      >
        {c.kind === "private" ? "You need an invitation to come back." : "You can join again from Browse channels."}
      </ConfirmDialog>
    </div>
  );
}

function Notice({ text, children }: { text: string; children?: ReactNode }) {
  return (
    <div className="flex min-h-tap-xl flex-wrap items-center justify-center gap-3 border-t border-border bg-surface px-gutter py-3 lg:px-5">
      <span className="type-label text-fg-muted">{text}</span>
      {children}
    </div>
  );
}
