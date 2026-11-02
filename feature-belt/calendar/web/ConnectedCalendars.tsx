"use client";

import {
  CALENDAR_COLORS,
  connectionFixPath,
  errorMessage,
  sourceProductLabel,
  useUpdateCalendarConnectedCalendar,
  type CalendarConnectedCalendar,
  type CalendarSource,
  type CalendarSources,
} from "@scaffoldapp/shared";
import { ConnectPrompt, IconButton, Menu, MenuItem, MenuLabel, MenuSeparator, ProviderIcon, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import { Check, Lock, MoreHorizontal, Plus } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CALENDAR_COLOR_CLASSES, CALENDAR_COLOR_NAMES } from "./parts";

/**
 * The calendar sidebar's "Connected" section: each connected account (Google Calendar,
 * Outlook) with its status, and under it each calendar with a visibility checkbox in its
 * colour and a ⋮ for colour and "counts for availability". With nothing connected, a quiet
 * offer to connect (never a nag).
 */
export function ConnectedCalendars({ sources, loading }: { sources: CalendarSources | undefined; loading: boolean }) {
  const pathname = usePathname();
  const connected = (sources?.sources ?? []).filter((s) => s.type === "connected");
  if (loading && !sources) {
    return (
      <div className="flex flex-col gap-2 px-1" aria-busy>
        <Skeleton className="h-6" />
        <Skeleton className="h-6 w-3/4" />
      </div>
    );
  }
  if (connected.length === 0) {
    return <ConnectPrompt offers={sources?.offers} returnTo={pathname} linkAs={Link} title="Your other calendars" info="See Google Calendar or Outlook events here, change them, and use their busy time for booking links." />;
  }
  return (
    <section aria-label="Connected" className="flex flex-col gap-2">
      <div className="flex min-h-8 items-center gap-1 px-1">
        <h2 className="min-w-0 flex-1 truncate type-label font-semibold text-fg-muted">Connected</h2>
        <Link
          href={connectionFixPath("calendar.google.read", pathname)}
          aria-label="Connect an account"
          className="relative inline-flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring before:absolute before:-inset-2 before:content-['']"
        >
          <Plus className="size-4" aria-hidden />
        </Link>
      </div>
      <ul className="flex flex-col gap-3">
        {connected.map((s) => (
          <ConnectedAccount key={s.id} source={s} />
        ))}
      </ul>
    </section>
  );
}

function ConnectedAccount({ source: s }: { source: CalendarSource }) {
  const pathname = usePathname();
  const product = sourceProductLabel(s.provider ?? "", s.product);
  const fix = s.fix_url ?? s.error?.fix_url;
  return (
    <li className="flex flex-col gap-1">
      <div className="flex min-h-8 items-center gap-2 px-1">
        <ProviderIcon provider={s.provider ?? ""} product={s.product} size={16} className="shrink-0 text-fg" />
        <span className="min-w-0 flex-1 truncate type-label text-fg" title={`${product} · ${s.account ?? ""}`}>
          {product}
          {s.account ? <span className="text-fg-muted"> · {s.account}</span> : null}
        </span>
        {s.status === "blocked" ? <Lock aria-label="Blocked by your workspace" className="size-4 shrink-0 text-fg-subtle" /> : null}
        {s.status === "needs_reauth" || (s.error && fix) ? (
          <Link
            href={fix ?? connectionFixPath(s.capability, pathname)}
            className="shrink-0 rounded-sm px-1.5 py-1 type-caption font-semibold text-warning-strong hover:bg-warning-soft focus-visible:outline-2 focus-visible:outline-ring"
          >
            Reconnect
          </Link>
        ) : null}
        <ResourceMenu
          feature={`connectors:${s.id}`}
          title={`${product} · ${s.account ?? ""}`}
          label={`More actions for ${product} · ${s.account ?? ""}`}
          actions={[
            ...(!s.can_write && s.write_url ? [{ id: "write", label: `Allow changes to ${product}`, icon: Check, onSelect: () => window.location.assign(s.write_url!) }] : []),
          ]}
        />
      </div>
      {s.status === "blocked" ? (
        <p className="px-1 type-caption text-fg-muted">Your workspace doesn&apos;t allow this account here.</p>
      ) : s.error && !fix ? (
        <p className="px-1 type-caption text-danger-strong">{s.error.message}</p>
      ) : (
        <ul className="flex flex-col">
          {s.calendars.map((c) => (
            <ConnectedCalendarRow key={c.id} source={s} calendar={c} />
          ))}
        </ul>
      )}
    </li>
  );
}

function ConnectedCalendarRow({ source, calendar: c }: { source: CalendarSource; calendar: CalendarConnectedCalendar }) {
  const update = useUpdateCalendarConnectedCalendar();
  const ref = { connection_id: source.id, calendar_id: c.id };
  const save = (body: { color?: string; hidden?: boolean; busy?: boolean }, done?: string) =>
    update.mutate(
      { calendar: ref, body },
      { onSuccess: () => done && toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't save that. Try again.")) },
    );
  const name = c.primary && c.name === source.account ? "Main calendar" : c.name;
  const colors = CALENDAR_COLOR_CLASSES[c.color] ?? CALENDAR_COLOR_CLASSES.info;
  return (
    <li className="group flex min-h-control items-center gap-2 rounded-md pl-1 hover:bg-sunken">
      <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-1 type-label text-fg">
        <input
          type="checkbox"
          className="peer sr-only"
          checked={!c.hidden}
          onChange={(e) => save({ hidden: !e.target.checked })}
          aria-label={`Show ${name}`}
        />
        <span
          aria-hidden
          className={cn(
            "flex size-4 shrink-0 items-center justify-center rounded-xs border-2 peer-focus-visible:outline-2 peer-focus-visible:outline-ring",
            c.hidden ? "border-border-strong bg-surface" : cn("border-transparent", colors.dot),
          )}
        >
          {c.hidden ? null : <Check className="size-3 text-surface" strokeWidth={3} />}
        </span>
        <span className={cn("min-w-0 flex-1 truncate", c.hidden && "text-fg-muted")}>{name}</span>
      </label>
      <Menu
        trigger={<IconButton label={`Options for ${name}`} icon={<MoreHorizontal />} variant="ghost" size="sm" className="opacity-100 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-visible:opacity-100" />}
      >
        <MenuLabel>Color</MenuLabel>
        {CALENDAR_COLORS.map((col) => (
          <MenuItem key={col} icon={<span className={cn("size-3 rounded-pill", CALENDAR_COLOR_CLASSES[col].dot)} aria-hidden />} onSelect={() => save({ color: col })}>
            {CALENDAR_COLOR_NAMES[col]}
            {c.color === col ? " ✓" : ""}
          </MenuItem>
        ))}
        <MenuSeparator />
        <MenuItem
          icon={c.busy ? <Check className="size-4" aria-hidden /> : <span className="size-4" aria-hidden />}
          onSelect={() => save({ busy: !c.busy }, c.busy ? "Its busy time no longer blocks booking links" : "Its busy time now blocks booking links")}
        >
          Counts for availability
        </MenuItem>
      </Menu>
    </li>
  );
}
