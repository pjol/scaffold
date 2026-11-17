"use client";

import {
  channelsSections,
  errorMessage,
  useChannelsWorkspace,
  useCreateChannelsSection,
  useDeleteChannelsSection,
  useMoveChannelToSection,
  useUpdateChannelsSection,
  useUpdateChannelsUserPrefs,
  type ChannelsBuiltinSection,
  type ChannelsSidebarChannel,
  type ChannelsSidebarSection,
} from "@scaffoldapp/shared";
import { Badge, Button, Dialog, ErrorState, Field, IconButton, Input, Menu, MenuItem, MenuSeparator, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import {
  ArrowDown,
  ArrowDownAZ,
  ArrowUp,
  AtSign,
  Bookmark,
  ChevronDown,
  Compass,
  FolderPlus,
  Inbox,
  Keyboard,
  MessageSquarePlus,
  MessagesSquare,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  SendHorizontal,
  Settings2,
  SquarePen,
  Trash2,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type DragEvent, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { PresenceAvatar } from "@/features/presence";
import { ChannelIcon } from "./ChannelScreen";
import { NewChannelDialog, NewMessageDialog } from "./Dialogs";
import { PreferencesDialog } from "./PreferencesDialog";
import { openShortcutsSheet } from "./Shortcuts";

function count(n: number): string {
  return n > 99 ? "99+" : String(n);
}

function Row({ href, active, icon, label, bold, muted, badge, badgeLabel, draggableId }: { href: string; active: boolean; icon: ReactNode; label: string; bold?: boolean; muted?: boolean; badge?: number; badgeLabel?: string; draggableId?: string }) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      draggable={!!draggableId}
      onDragStart={draggableId ? (e) => e.dataTransfer.setData("application/x-channels-id", draggableId) : undefined}
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
      draggableId={c.id}
    />
  );
}

/** A collapsible group; channels can be dropped on it to move them in. */
function SectionGroup({ title, open, onToggle, action, children, empty, onDropChannel }: { title: string; open: boolean; onToggle: () => void; action?: ReactNode; children: ReactNode; empty?: boolean; onDropChannel?: (channelId: string) => void }) {
  const [over, setOver] = useState(false);
  const drop = onDropChannel
    ? {
        onDragOver: (e: DragEvent) => {
          if (!e.dataTransfer.types.includes("application/x-channels-id")) return;
          e.preventDefault();
          setOver(true);
        },
        onDragLeave: () => setOver(false),
        onDrop: (e: DragEvent) => {
          setOver(false);
          const id = e.dataTransfer.getData("application/x-channels-id");
          if (id) onDropChannel(id);
        },
      }
    : {};
  return (
    <section className={cn("flex flex-col gap-0.5 rounded-md", over && "bg-primary-soft")} {...drop}>
      <div className="flex min-h-control items-center gap-1 px-1">
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex min-w-0 flex-1 items-center gap-1 rounded-md px-2 py-1 text-left type-caption font-semibold text-fg-muted transition-colors hover:text-fg focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ChevronDown aria-hidden className={cn("size-4 shrink-0 transition-transform motion-reduce:transition-none", !open && "-rotate-90")} />
          <span className="truncate">{title}</span>
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

function SectionDialog({ section, orgId, onOpenChange }: { section: ChannelsSidebarSection | "new" | null; orgId: string; onOpenChange: (o: boolean) => void }) {
  const create = useCreateChannelsSection(orgId);
  const update = useUpdateChannelsSection(orgId);
  const [name, setName] = useState("");
  useEffect(() => {
    if (section) setName(section === "new" ? "" : section.name);
  }, [section]);
  const isNew = section === "new";
  return (
    <Dialog
      open={section !== null}
      onOpenChange={onOpenChange}
      title={isNew ? "New section" : "Rename section"}
      info="Sections are yours alone. Drag conversations into them, or use Move to section in a conversation's menu."
      footer={
        <Button
          disabled={!name.trim()}
          loading={create.isPending || update.isPending}
          onClick={() => {
            const done = { onSuccess: () => onOpenChange(false), onError: (e: unknown) => toast.error(errorMessage(e)) };
            if (isNew) create.mutate(name.trim(), done);
            else if (section) update.mutate({ id: section.id, name: name.trim() }, done);
          }}
        >
          {isNew ? "Create section" : "Save"}
        </Button>
      }
    >
      <Field label="Name">
        <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={40} />
      </Field>
    </Dialog>
  );
}

/** The workspace sidebar: Activity, Threads, Later, Drafts, All unread; starred, your sections, channels, direct messages. */
export function Sidebar({ orgId, orgName }: { orgId: string; orgName: string }) {
  const pathname = usePathname() ?? "";
  const router = useRouter();
  const ws = useChannelsWorkspace(orgId);
  const setPrefs = useUpdateChannelsUserPrefs(orgId);
  const updateSection = useUpdateChannelsSection(orgId);
  const deleteSection = useDeleteChannelsSection(orgId);
  const move = useMoveChannelToSection(orgId);
  const [creating, setCreating] = useState(false);
  const [messaging, setMessaging] = useState(false);
  const [sectionDialog, setSectionDialog] = useState<ChannelsSidebarSection | "new" | null>(null);
  const [prefsOpen, setPrefsOpen] = useState(false);
  const activeId = pathname.startsWith("/channels/") ? pathname.split("/")[2] : null;
  const canCreate = !!ws.data && (ws.data.permissions.create_public || ws.data.permissions.create_private);
  const prefs = ws.data?.prefs;
  const collapsed = new Set(prefs?.collapsed ?? []);
  const toastErr = (e: unknown) => toast.error(errorMessage(e));
  const toggleBuiltin = (id: ChannelsBuiltinSection) => {
    const next = collapsed.has(id) ? [...collapsed].filter((x) => x !== id) : [...collapsed, id];
    setPrefs.mutate({ collapsed: next }, { onError: toastErr });
  };
  const moveTo = (sectionId: string | null) => (channelId: string) => move.mutate({ channelId, sectionId }, { onError: toastErr });
  const swapSections = (list: ChannelsSidebarSection[], a: number, b: number) => {
    const x = list[a];
    const y = list[b];
    if (!x || !y) return;
    updateSection.mutate({ id: x.id, position: b }, { onError: toastErr });
    updateSection.mutate({ id: y.id, position: a }, { onError: toastErr });
  };

  const sectionMenu: ResourceMenuAction[] = [
    ...(canCreate ? [{ id: "new-channel", label: "Create a channel", icon: Plus, onSelect: () => setCreating(true) }] : []),
    { id: "new-message", label: "New message", icon: SquarePen, onSelect: () => setMessaging(true) },
    { id: "new-section", label: "New section", icon: FolderPlus, onSelect: () => setSectionDialog("new") },
    {
      id: "sort",
      label: "Sort",
      icon: ArrowDownAZ,
      hint: prefs?.sidebar_sort === "recent" ? "Recent activity" : "A–Z",
      choices: [
        { id: "alpha", label: "A–Z", checked: prefs?.sidebar_sort !== "recent", onSelect: () => setPrefs.mutate({ sidebar_sort: "alpha" }, { onError: toastErr }) },
        { id: "recent", label: "Recent activity", checked: prefs?.sidebar_sort === "recent", onSelect: () => setPrefs.mutate({ sidebar_sort: "recent" }, { onError: toastErr }) },
      ],
    },
    { id: "prefs", label: "Preferences", icon: Settings2, onSelect: () => setPrefsOpen(true) },
    { id: "shortcuts", label: "Keyboard shortcuts", icon: Keyboard, onSelect: openShortcutsSheet },
  ];

  let body: ReactNode;
  if (ws.isPending) body = <SidebarSkeleton />;
  else if (ws.isError) body = <ErrorState compact title="Couldn't load channels" info={ws.error.message} onRetry={() => void ws.refetch()} retrying={ws.isFetching} />;
  else {
    const data = ws.data;
    const { starred, custom, channels, direct } = channelsSections(data.channels, data.prefs.sidebar_sort, data.sections);
    const unreadCount = data.channels.filter((c) => c.unread > 0 && !c.muted).length;
    const list = (items: ChannelsSidebarChannel[]) => (
      <ul className="flex flex-col gap-0.5">
        {items.map((c) => (
          <li key={c.id}>
            <ChannelRow c={c} active={c.id === activeId} />
          </li>
        ))}
      </ul>
    );
    body = (
      <nav aria-label="Channels" className="flex flex-col gap-4 p-2">
        <div className="flex flex-col gap-0.5">
          <Row href="/channels/activity" active={pathname === "/channels/activity"} icon={<AtSign aria-hidden />} label="Activity" bold={data.activity_unread > 0} badge={data.activity_unread} badgeLabel={`${data.activity_unread} new`} />
          <Row href="/channels/unreads" active={pathname === "/channels/unreads"} icon={<Inbox aria-hidden />} label={unreadCount > 0 ? `All unread · ${unreadCount}` : "All unread"} bold={unreadCount > 0} />
          <Row href="/channels/threads" active={pathname === "/channels/threads"} icon={<MessagesSquare aria-hidden />} label="Threads" bold={data.threads_unread > 0} />
          <Row href="/channels/later" active={pathname === "/channels/later"} icon={<Bookmark aria-hidden />} label={data.later_count > 0 ? `Later · ${data.later_count}` : "Later"} />
          <Row
            href="/channels/drafts"
            active={pathname === "/channels/drafts"}
            icon={<SendHorizontal aria-hidden />}
            label={data.draft_count + data.scheduled_count > 0 ? `Drafts & sent · ${data.draft_count + data.scheduled_count}` : "Drafts & sent"}
          />
          <Row href="/channels/browse" active={pathname === "/channels/browse"} icon={<Compass aria-hidden />} label="Browse channels" />
        </div>
        {starred.length > 0 && (
          <SectionGroup title="Starred" open={!collapsed.has("starred")} onToggle={() => toggleBuiltin("starred")}>
            {list(starred)}
          </SectionGroup>
        )}
        {data.sections.map((sec, i) => (
          <SectionGroup
            key={sec.id}
            title={sec.name}
            open={!sec.collapsed}
            onToggle={() => updateSection.mutate({ id: sec.id, collapsed: !sec.collapsed }, { onError: toastErr })}
            empty={(custom[sec.id] ?? []).length === 0}
            onDropChannel={moveTo(sec.id)}
            action={
              <Menu trigger={<IconButton size="sm" variant="ghost" label={`${sec.name} section actions`} icon={<MoreHorizontal />} />}>
                <MenuItem icon={<Pencil />} onSelect={() => setSectionDialog(sec)}>
                  Rename section
                </MenuItem>
                {i > 0 && (
                  <MenuItem icon={<ArrowUp />} onSelect={() => swapSections(data.sections, i, i - 1)}>
                    Move up
                  </MenuItem>
                )}
                {i < data.sections.length - 1 && (
                  <MenuItem icon={<ArrowDown />} onSelect={() => swapSections(data.sections, i, i + 1)}>
                    Move down
                  </MenuItem>
                )}
                <MenuSeparator />
                <MenuItem icon={<Trash2 />} destructive onSelect={() => deleteSection.mutate(sec.id, { onSuccess: () => toast.success(`Removed ${sec.name}. Its conversations moved back.`), onError: toastErr })}>
                  Remove section
                </MenuItem>
              </Menu>
            }
          >
            {list(custom[sec.id] ?? [])}
          </SectionGroup>
        ))}
        <SectionGroup
          title="Channels"
          open={!collapsed.has("channels")}
          onToggle={() => toggleBuiltin("channels")}
          onDropChannel={moveTo(null)}
          action={canCreate ? <IconButton size="sm" variant="ghost" label="Create a channel" icon={<Plus />} onClick={() => setCreating(true)} /> : undefined}
        >
          {channels.length === 0 ? (
            <Button variant="ghost" size="sm" leftIcon={<Compass />} asChild>
              <Link href="/channels/browse">Browse channels</Link>
            </Button>
          ) : (
            list(channels)
          )}
        </SectionGroup>
        <SectionGroup
          title="Direct messages"
          open={!collapsed.has("direct")}
          onToggle={() => toggleBuiltin("direct")}
          onDropChannel={moveTo(null)}
          action={<IconButton size="sm" variant="ghost" label="New message" icon={<Plus />} onClick={() => setMessaging(true)} />}
        >
          {direct.length === 0 ? (
            <Button variant="ghost" size="sm" leftIcon={<MessageSquarePlus />} onClick={() => setMessaging(true)}>
              Message someone
            </Button>
          ) : (
            list(direct)
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
      <SectionDialog section={sectionDialog} orgId={orgId} onOpenChange={(o) => !o && setSectionDialog(null)} />
      {prefsOpen && <PreferencesDialog orgId={orgId} open onOpenChange={setPrefsOpen} />}
    </div>
  );
}
