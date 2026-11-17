"use client";

import {
  errorMessage,
  groupLabel,
  notificationSnoozeOptions,
  useInbox,
  useMarkNotificationsRead,
  useMarkNotificationsUnread,
  useMuteNotificationResource,
  useNotificationGroups,
  useNotificationKinds,
  useNow,
  useSnoozeNotifications,
  useUnreadNotificationCount,
  useUnsnoozeNotifications,
  type NotificationGroup,
  type NotificationItem,
  type NotificationTargets,
} from "@scaffoldapp/shared";
import {
  Button,
  Card,
  Chip,
  EmptyState,
  ErrorState,
  IconButton,
  Menu,
  MenuItem,
  MenuSeparator,
  MenuSub,
  SegmentedControl,
  Skeleton,
  SkeletonGroup,
  Stagger,
  StaggerItem,
  Switch,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { AlarmClock, Bell, BellOff, CheckCheck, Clock, Mail, MailOpen, MoreVertical, Settings, Undo2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { safeAppPath } from "./toasts";

type View = "all" | "unread" | "snoozed";

const untilFmt = new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });

/** One row's actions: read/unread, snooze or bring back, mute what it is about. */
function RowActions({ item, target, unread, count }: { item: NotificationItem; target: NotificationTargets; unread: boolean; count: number }) {
  const markRead = useMarkNotificationsRead();
  const markUnread = useMarkNotificationsUnread();
  const snooze = useSnoozeNotifications();
  const unsnooze = useUnsnoozeNotifications();
  const mute = useMuteNotificationResource();
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't change it. Try again."));
  const what = count > 1 ? `${count} notifications` : "Notification";
  return (
    <Menu align="end" trigger={<IconButton label={`Actions for “${item.title}”`} icon={<MoreVertical />} variant="ghost" size="sm" />}>
      {unread ? (
        <MenuItem icon={<MailOpen />} onSelect={() => markRead.mutate(target, { onError: fail })}>
          Mark read
        </MenuItem>
      ) : (
        <MenuItem icon={<Mail />} onSelect={() => markUnread.mutate(target, { onError: fail })}>
          Mark unread
        </MenuItem>
      )}
      {item.snoozed_until ? (
        <MenuItem icon={<Undo2 />} onSelect={() => unsnooze.mutate(target, { onSuccess: () => toast.success(`${what} back in your inbox.`), onError: fail })}>
          Unsnooze
        </MenuItem>
      ) : (
        <MenuSub icon={<AlarmClock />} label="Snooze">
          {notificationSnoozeOptions().map((o) => (
            <MenuItem
              key={o.label}
              onSelect={() =>
                snooze.mutate({ ...target, until: o.until }, { onSuccess: () => toast.success(`${what} snoozed until ${untilFmt.format(new Date(o.until))}.`), onError: fail })
              }
            >
              <span className="flex min-w-0 flex-1 items-center justify-between gap-3">
                {o.label}
                <span className="type-caption text-fg-muted">{untilFmt.format(new Date(o.until))}</span>
              </span>
            </MenuItem>
          ))}
        </MenuSub>
      )}
      {item.mutable && item.resource ? (
        <>
          <MenuSeparator />
          <MenuItem
            icon={<BellOff />}
            onSelect={() => mute.mutate({ resource: item.resource, label: item.title }, { onSuccess: () => toast.success("Muted. Undo it in notification settings."), onError: fail })}
          >
            {item.resource.includes("/thread:") ? "Mute this thread" : "Mute notifications about this"}
          </MenuItem>
        </>
      ) : null}
    </Menu>
  );
}

function Row({
  item,
  now,
  onOpen,
  count = 1,
  unreadCount,
  target,
}: {
  item: NotificationItem;
  now: number;
  onOpen: () => void;
  count?: number;
  unreadCount?: number;
  target: NotificationTargets;
}) {
  const unread = unreadCount !== undefined ? unreadCount > 0 : !item.read_at;
  return (
    <div className="group flex items-start gap-1 rounded-md transition-colors focus-within:bg-sunken hover:bg-sunken">
      <button type="button" onClick={onOpen} className="flex min-h-row min-w-0 flex-1 items-start gap-3 rounded-md py-3 pl-4 text-left focus-visible:outline-2 focus-visible:outline-ring">
        <span className={cn("mt-2 size-2 shrink-0 rounded-pill", unread ? "bg-primary" : "bg-transparent")} aria-hidden />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex items-baseline gap-3">
            <span className={cn("min-w-0 flex-1 type-body text-fg", unread && "font-semibold")}>
              {unread && <span className="sr-only">Unread: </span>}
              {item.title}
            </span>
            {count > 1 ? (
              <span className="shrink-0 rounded-pill bg-sunken px-2 type-caption text-fg-muted group-hover:bg-surface" aria-label={`${count} notifications`}>
                {count}
              </span>
            ) : null}
            <time dateTime={item.sort_at} title={formatDateTime(item.created_at)} className="shrink-0 type-caption text-fg-muted">
              {formatRelative(item.sort_at, now)}
            </time>
          </span>
          {item.body && <span className="line-clamp-2 type-label whitespace-pre-line text-fg-muted">{item.body}</span>}
          {item.snoozed_until ? (
            <span className="flex items-center gap-1 type-caption text-fg-muted">
              <Clock aria-hidden className="size-3.5" />
              Back {untilFmt.format(new Date(item.snoozed_until))}
            </span>
          ) : null}
        </span>
      </button>
      <span className="shrink-0 py-2 pr-2">
        <RowActions item={item} target={target} unread={unread} count={count} />
      </span>
    </div>
  );
}

function InboxSkeleton() {
  return (
    <SkeletonGroup label="Loading notifications" className="flex flex-col">
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="flex items-start gap-3 px-4 py-3">
          <Skeleton className="mt-2 size-2 rounded-pill" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton shape="text" className="h-5 w-2/3" />
            <Skeleton shape="text" className="h-4 w-full" />
          </div>
        </div>
      ))}
    </SkeletonGroup>
  );
}

function Empty({ view, kind }: { view: View; kind: string }) {
  return (
    <EmptyState
      compact
      icon={view === "unread" ? <BellOff /> : view === "snoozed" ? <AlarmClock /> : <Bell />}
      title={view === "unread" ? "Nothing unread" : view === "snoozed" ? "Nothing snoozed" : kind ? `No ${groupLabel(kind)} notifications` : "No notifications yet"}
      action={
        <Button asChild variant="secondary" size="auto" leftIcon={<Settings />}>
          <Link href="/settings/notifications">Notification settings</Link>
        </Button>
      }
    />
  );
}

/**
 * /notifications: the inbox, newest first. All / Unread / Snoozed and a feature filter (in the
 * URL), grouped by thread or resource on request; each row can be read or unread, snoozed and,
 * where its feature allows, muted. Opening a row marks it read and follows its link.
 */
export function InboxScreen() {
  const router = useRouter();
  const [view, setView] = useUrlState<View>("filter", "all");
  const [kind, setKind] = useUrlState<string>("kind", "");
  const [grouped, setGrouped] = useUrlState<"1" | "">("grouped", "");
  const filter = { unread: view === "unread", snoozed: view === "snoozed", kind };
  const inbox = useInbox(filter, { enabled: grouped !== "1" });
  const groups = useNotificationGroups(filter, { enabled: grouped === "1" });
  const kinds = useNotificationKinds(view === "snoozed");
  const unread = useUnreadNotificationCount() ?? 0;
  const markRead = useMarkNotificationsRead();
  const now = useNow(60_000);
  const list = grouped === "1" ? groups : inbox;

  const open = (item: NotificationItem, target: NotificationTargets, isUnread: boolean) => {
    if (isUnread) markRead.mutate(target);
    const path = safeAppPath(item.link);
    if (path) router.push(path);
  };
  const groupTarget = (g: NotificationGroup): NotificationTargets => ({ resources: [g.key] });
  const chips = kinds.data?.items ?? [];

  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen
        title="Inbox"
        className="max-w-prose"
        action={
          <Button
            variant="ghost"
            size="sm"
            leftIcon={<CheckCheck />}
            disabled={unread === 0 || markRead.isPending || view === "snoozed"}
            onClick={() => markRead.mutate({ all: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't mark them read. Try again.")) })}
          >
            Mark all read
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <SegmentedControl<View>
              aria-label="Show"
              value={view}
              onValueChange={setView}
              options={[
                { value: "all", label: "All" },
                { value: "unread", label: unread > 0 ? `Unread (${unread})` : "Unread" },
                { value: "snoozed", label: kinds.data?.snoozed ? `Snoozed (${kinds.data.snoozed})` : "Snoozed" },
              ]}
            />
            <Switch checked={grouped === "1"} onCheckedChange={(on) => setGrouped(on ? "1" : "")} label="Group by thread" />
          </div>
          {chips.length > 1 || kind ? (
            <div className="flex flex-wrap gap-2" role="group" aria-label="Filter by feature">
              <Chip selected={!kind} onClick={() => setKind("")}>
                Everything
              </Chip>
              {chips.map((c) => (
                <Chip key={c.group} selected={kind === c.group} count={view === "unread" ? c.unread : c.count} onClick={() => setKind(kind === c.group ? "" : c.group)}>
                  {groupLabel(c.group)}
                </Chip>
              ))}
            </div>
          ) : null}
          <Card padding="none" className="p-1">
            {list.isPending ? (
              <InboxSkeleton />
            ) : list.isError ? (
              <ErrorState compact title="Couldn't load notifications" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
            ) : grouped === "1" ? (
              (groups.data?.items.length ?? 0) === 0 ? (
                <Empty view={view} kind={kind} />
              ) : (
                <>
                  <Stagger as="ul" className="flex flex-col" aria-label="Notifications by thread">
                    {groups.data?.items.map((g) => (
                      <StaggerItem as="li" key={g.key}>
                        <Row item={g.latest} now={now} count={g.count} unreadCount={g.unread} target={groupTarget(g)} onOpen={() => open(g.latest, groupTarget(g), g.unread > 0)} />
                      </StaggerItem>
                    ))}
                  </Stagger>
                  <LoadMore hasNextPage={groups.hasNextPage} isFetchingNextPage={groups.isFetchingNextPage} fetchNextPage={groups.fetchNextPage} />
                </>
              )
            ) : (inbox.data?.items.length ?? 0) === 0 ? (
              <Empty view={view} kind={kind} />
            ) : (
              <>
                <Stagger as="ul" className="flex flex-col" aria-label="Notifications">
                  {inbox.data?.items.map((item) => (
                    <StaggerItem as="li" key={item.id}>
                      <Row item={item} now={now} target={{ ids: [item.id] }} onOpen={() => open(item, { ids: [item.id] }, !item.read_at)} />
                    </StaggerItem>
                  ))}
                </Stagger>
                <LoadMore hasNextPage={inbox.hasNextPage} isFetchingNextPage={inbox.isFetchingNextPage} fetchNextPage={inbox.fetchNextPage} />
              </>
            )}
          </Card>
        </div>
      </Screen>
    </div>
  );
}
