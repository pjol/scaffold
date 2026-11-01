"use client";

import { channelsSections, useChannelsWorkspace, type ChannelsSidebarChannel } from "@scaffoldapp/shared";
import { Badge, Button, ErrorState, IconButton, Skeleton, cn } from "@scaffoldapp/ui-web";
import { Bookmark, ChevronDown, Compass, MessageSquarePlus, MessagesSquare, Plus, Search, SquarePen } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { PresenceAvatar } from "@/features/presence";
import { ChannelIcon } from "./ChannelScreen";
import { NewChannelDialog, NewMessageDialog } from "./Dialogs";

function count(n: number): string {
  return n > 99 ? "99+" : String(n);
}

function Row({ href, active, icon, label, bold, muted, badge, badgeLabel }: { href: string; active: boolean; icon: ReactNode; label: string; bold?: boolean; muted?: boolean; badge?: number; badgeLabel?: string }) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex min-h-control items-center gap-2 rounded-md px-3 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring pointer-coarse:min-h-tap",
        active ? "bg-primary text-primary-fg" : "hover:bg-sunken",
      )}
    >
      <span className={cn("flex size-5 shrink-0 items-center justify-center [&_svg]:size-4", active ? "text-primary-fg" : muted ? "text-fg-subtle" : "text-fg-muted")}>{icon}</span>
      <span className={cn("min-w-0 flex-1 truncate type-label", active ? "text-primary-fg" : bold ? "font-semibold text-fg" : muted ? "text-fg-subtle" : "text-fg-muted")}>{label}</span>
      {badge ? (
        <Badge tone="danger" variant="solid" aria-label={badgeLabel ?? `${badge}`}>
          {count(badge)}
        </Badge>
      ) : null}
    </Link>
  );
}

function ChannelRow({ c, active }: { c: ChannelsSidebarChannel; active: boolean }) {
  const direct = c.kind === "dm" || c.kind === "group_dm";
  const peer = c.kind === "dm" ? c.people[0] : undefined;
  const icon = peer ? (
    <PresenceAvatar userId={peer.deleted ? null : peer.id} name={peer.display_name} src={peer.avatar_url} size="xs" />
  ) : c.kind === "dm" ? (
    <MessagesSquare aria-hidden />
  ) : (
    <ChannelIcon channel={c} />
  );
  return (
    <Row
      href={`/channels/${c.id}`}
      active={active}
      icon={icon}
      label={c.name}
      bold={c.unread > 0 && !c.muted}
      muted={c.muted}
      badge={c.mentions}
      badgeLabel={direct ? `${c.mentions} unread` : `${c.mentions} mentions`}
    />
  );
}

function SectionGroup({ title, action, children, empty }: { title: string; action?: ReactNode; children: ReactNode; empty?: boolean }) {
  const [open, setOpen] = useState(true);
  return (
    <section className="flex flex-col gap-0.5">
      <div className="flex min-h-control items-center gap-1 px-1">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          className="flex min-w-0 flex-1 items-center gap-1 rounded-md px-2 py-1 text-left type-caption font-semibold text-fg-muted transition-colors hover:text-fg focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ChevronDown aria-hidden className={cn("size-4 transition-transform motion-reduce:transition-none", !open && "-rotate-90")} />
          {title}
        </button>
        {action}
      </div>
      {open && !empty && children}
    </section>
  );
}

function SidebarSkeleton() {
  return (
    <div className="flex flex-col gap-2 p-3" aria-hidden>
      {Array.from({ length: 8 }, (_, i) => (
        <Skeleton key={i} className={cn("h-6", i % 3 === 0 ? "w-1/2" : "w-3/4")} />
      ))}
    </div>
  );
}

/** The workspace sidebar: Threads, Later, starred, channels, direct messages; create and browse. */
export function Sidebar({ orgId, orgName }: { orgId: string; orgName: string }) {
  const pathname = usePathname() ?? "";
  const router = useRouter();
  const ws = useChannelsWorkspace(orgId);
  const [creating, setCreating] = useState(false);
  const [messaging, setMessaging] = useState(false);
  const activeId = pathname.startsWith("/channels/") ? pathname.split("/")[2] : null;
  const canCreate = !!ws.data && (ws.data.permissions.create_public || ws.data.permissions.create_private);

  const sectionMenu: ResourceMenuAction[] = [
    ...(canCreate ? [{ id: "new-channel", label: "Create a channel", icon: Plus, onSelect: () => setCreating(true) }] : []),
    { id: "new-message", label: "New message", icon: SquarePen, onSelect: () => setMessaging(true) },
  ];

  let body: ReactNode;
  if (ws.isPending) body = <SidebarSkeleton />;
  else if (ws.isError) body = <ErrorState compact title="Couldn't load channels" info={ws.error.message} onRetry={() => void ws.refetch()} retrying={ws.isFetching} />;
  else {
    const { starred, channels, direct } = channelsSections(ws.data.channels);
    body = (
      <nav aria-label="Channels" className="flex flex-col gap-4 p-2">
        <div className="flex flex-col gap-0.5">
          <Row href="/channels/threads" active={pathname === "/channels/threads"} icon={<MessagesSquare aria-hidden />} label="Threads" bold={ws.data.threads_unread > 0} />
          <Row href="/channels/later" active={pathname === "/channels/later"} icon={<Bookmark aria-hidden />} label={ws.data.later_count > 0 ? `Later · ${ws.data.later_count}` : "Later"} />
          <Row href="/channels/browse" active={pathname === "/channels/browse"} icon={<Compass aria-hidden />} label="Browse channels" />
        </div>
        {starred.length > 0 && (
          <SectionGroup title="Starred">
            <ul className="flex flex-col gap-0.5">
              {starred.map((c) => (
                <li key={c.id}>
                  <ChannelRow c={c} active={c.id === activeId} />
                </li>
              ))}
            </ul>
          </SectionGroup>
        )}
        <SectionGroup title="Channels" action={canCreate ? <IconButton size="sm" variant="ghost" label="Create a channel" icon={<Plus />} onClick={() => setCreating(true)} /> : undefined}>
          {channels.length === 0 ? (
            <Button variant="ghost" size="sm" leftIcon={<Compass />} asChild>
              <Link href="/channels/browse">Browse channels</Link>
            </Button>
          ) : (
            <ul className="flex flex-col gap-0.5">
              {channels.map((c) => (
                <li key={c.id}>
                  <ChannelRow c={c} active={c.id === activeId} />
                </li>
              ))}
            </ul>
          )}
        </SectionGroup>
        <SectionGroup title="Direct messages" action={<IconButton size="sm" variant="ghost" label="New message" icon={<Plus />} onClick={() => setMessaging(true)} />}>
          {direct.length === 0 ? (
            <Button variant="ghost" size="sm" leftIcon={<MessageSquarePlus />} onClick={() => setMessaging(true)}>
              Message someone
            </Button>
          ) : (
            <ul className="flex flex-col gap-0.5">
              {direct.map((c) => (
                <li key={c.id}>
                  <ChannelRow c={c} active={c.id === activeId} />
                </li>
              ))}
            </ul>
          )}
        </SectionGroup>
      </nav>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-topbar items-center gap-1 border-b border-border px-gutter lg:px-3">
        <h1 className="min-w-0 flex-1 truncate type-title text-fg lg:type-title-sm">{orgName}</h1>
        <IconButton variant="ghost" label="Search messages" icon={<Search />} onClick={() => router.push("/channels/search")} />
        <IconButton variant="soft" label="New message" icon={<SquarePen />} onClick={() => setMessaging(true)} />
        <ResourceMenu feature="channels" orgId={orgId} title={orgName} actions={sectionMenu} label="Workspace actions" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{body}</div>
      <NewChannelDialog orgId={orgId} open={creating} onOpenChange={setCreating} />
      <NewMessageDialog orgId={orgId} open={messaging} onOpenChange={setMessaging} />
    </div>
  );
}
