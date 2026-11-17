"use client";

import {
  CALENDAR_COLORS,
  CALENDAR_SHARE_LEVEL_LABEL,
  errorMessage,
  orgsNoun,
  useAccessMyTeams,
  useActiveOrg,
  useCalendarCalendarPrefs,
  useCalendarCalendars,
  useCreateCalendarCalendar,
  useDeleteCalendarCalendar,
  useUpdateCalendarCalendar,
  type CalendarCalendar,
  type CalendarColor,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, Select, Skeleton, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Check, EyeOff, Palette, Plus, Settings2, Share2, Trash2, Users, X } from "lucide-react";
import { useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CalendarShareDialog } from "./CalendarShareDialog";
import { CALENDAR_COLOR_CLASSES, CALENDAR_COLOR_NAMES } from "./parts";

/**
 * The sidebar's "My calendars": your calendars, your teams' and colleagues' you added, each
 * with a show/hide checkbox in its colour and a ⋮ (share, settings, colour, hide, remove or
 * delete). + makes a calendar or adds one shared with you.
 */
export function CalendarsPanel() {
  const cals = useCalendarCalendars();
  const prefs = useCalendarCalendarPrefs();
  const [creating, setCreating] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const [sharing, setSharing] = useState<CalendarCalendar | null>(null);
  const [editing, setEditing] = useState<CalendarCalendar | null>(null);
  const [deleting, setDeleting] = useState<CalendarCalendar | null>(null);
  const del = useDeleteCalendarCalendar();
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't save that. Try again."));
  const items = cals.data?.items ?? [];
  const shared = cals.data?.shared ?? [];

  return (
    <section aria-label="My calendars" className="flex flex-col gap-2">
      <div className="flex min-h-8 items-center gap-1 px-1">
        <h2 className="min-w-0 flex-1 truncate type-label font-semibold text-fg-muted">My calendars</h2>
        <ResourceMenu
          feature="calendar"
          title="Calendars"
          label="Add a calendar"
          actions={[
            { id: "new", label: "New calendar", icon: Plus, onSelect: () => setCreating(true) },
            { id: "browse", label: shared.length > 0 ? `Shared with you (${shared.length})` : "Shared with you", icon: Users, onSelect: () => setBrowsing(true) },
          ]}
        />
      </div>
      {cals.isPending ? (
        <div className="flex flex-col gap-2 px-1" aria-busy>
          <Skeleton className="h-6" />
          <Skeleton className="h-6 w-2/3" />
        </div>
      ) : cals.isError ? (
        <p className="px-1 type-caption text-danger-strong">{errorMessage(cals.error, "Couldn't load your calendars.")}</p>
      ) : (
        <ul className="flex flex-col">
          {items.map((c) => (
            <li key={c.id} className="group flex min-h-control items-center gap-2 rounded-md pl-1 hover:bg-sunken">
              <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-1 type-label text-fg">
                <input type="checkbox" className="peer sr-only" checked={!c.hidden} onChange={(e) => prefs.mutate({ id: c.id, hidden: !e.target.checked }, { onError: fail })} aria-label={`Show ${c.name}`} />
                <span
                  aria-hidden
                  className={cn(
                    "flex size-4 shrink-0 items-center justify-center rounded-xs border-2 peer-focus-visible:outline-2 peer-focus-visible:outline-ring",
                    c.hidden ? "border-border-strong bg-surface" : cn("border-transparent", CALENDAR_COLOR_CLASSES[c.color].dot),
                  )}
                >
                  {c.hidden ? null : <Check className="size-3 text-surface" strokeWidth={3} />}
                </span>
                <span className={cn("min-w-0 flex-1 truncate", c.hidden && "text-fg-muted")} title={calendarSubtitle(c)}>
                  {c.name}
                  {c.team_name && c.team_name !== c.name ? <span className="text-fg-muted"> · {c.team_name}</span> : null}
                </span>
                {c.level === "view" ? <EyeOff className="size-3.5 shrink-0 text-fg-subtle" aria-label="Free/busy only" /> : null}
              </label>
              <ResourceMenu
                feature="calendar"
                title={c.name}
                label={`Options for ${c.name}`}
                className="opacity-100 sm:opacity-0 sm:group-hover:opacity-100 sm:focus-visible:opacity-100"
                actions={[
                  ...(c.level === "manage" ? [{ id: "share", label: "Share…", icon: Share2, onSelect: () => setSharing(c) }] : []),
                  ...(c.level === "manage" ? [{ id: "settings", label: "Settings", icon: Settings2, onSelect: () => setEditing(c) }] : []),
                  {
                    id: "color",
                    label: "Color",
                    icon: Palette,
                    hint: CALENDAR_COLOR_NAMES[c.color],
                    choices: CALENDAR_COLORS.map((col) => ({ id: col, label: CALENDAR_COLOR_NAMES[col], checked: c.color === col, onSelect: () => prefs.mutate({ id: c.id, color: col }, { onError: fail }) })),
                  },
                  ...(!c.mine
                    ? [{ id: "remove", label: "Remove from my list", icon: X, onSelect: () => prefs.mutate({ id: c.id, added: false }, { onSuccess: () => toast.success(`${c.name} removed from your list`), onError: fail }) }]
                    : []),
                  ...(c.mine && !c.is_primary ? [{ id: "delete", label: "Delete calendar", icon: Trash2, destructive: true, onSelect: () => setDeleting(c) }] : []),
                ]}
              />
            </li>
          ))}
        </ul>
      )}
      {creating ? <CalendarFormDialog onClose={() => setCreating(false)} /> : null}
      {editing ? <CalendarFormDialog calendar={editing} onClose={() => setEditing(null)} /> : null}
      {sharing ? <CalendarShareDialog calendar={sharing} open onOpenChange={(o) => !o && setSharing(null)} /> : null}
      <SharedCalendarsDialog open={browsing} onOpenChange={setBrowsing} calendars={shared} loading={cals.isPending} error={cals.isError ? cals.error.message : null} onRetry={() => void cals.refetch()} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete calendar"
        tone="danger"
        onConfirm={async () => {
          if (!deleting) return;
          await del.mutateAsync(deleting.id);
          toast.success("Calendar deleted");
          setDeleting(null);
        }}
      >
        <p className="type-body text-fg-muted">Its events are deleted and their guests told. Booked meetings move to their host&apos;s main calendar.</p>
      </ConfirmDialog>
    </section>
  );
}

function calendarSubtitle(c: CalendarCalendar): string {
  const who = c.mine ? "Yours" : c.team_name ? `Team: ${c.team_name}` : c.owner ? c.owner.display_name : "";
  return `${c.name} · ${who} · ${CALENDAR_SHARE_LEVEL_LABEL[c.level]}`;
}

/** New calendar (in a workspace, optionally for a team), or a calendar's settings. */
function CalendarFormDialog({ calendar, onClose }: { calendar?: CalendarCalendar; onClose: () => void }) {
  const { org, orgs } = useActiveOrg();
  const [orgId, setOrgId] = useState(calendar?.org_id ?? org?.id ?? orgs[0]?.id ?? "");
  const teams = useAccessMyTeams(calendar ? null : orgId || null);
  const [name, setName] = useState(calendar?.name ?? "");
  const [description, setDescription] = useState(calendar?.description ?? "");
  const [color, setColor] = useState<CalendarColor>(calendar?.own_color ?? "accent");
  const [teamId, setTeamId] = useState("");
  const [countsBusy, setCountsBusy] = useState(calendar?.counts_busy ?? true);
  const [error, setError] = useState<string | null>(null);
  const create = useCreateCalendarCalendar();
  const update = useUpdateCalendarCalendar();

  const submit = async () => {
    if (!name.trim()) {
      setError("Name the calendar.");
      return;
    }
    try {
      if (calendar) {
        await update.mutateAsync({ id: calendar.id, body: { name: name.trim(), description, color, counts_busy: countsBusy } });
        toast.success("Calendar saved");
      } else {
        await create.mutateAsync({ name: name.trim(), description, color, org_id: orgId, ...(teamId ? { team_id: teamId, counts_busy: false } : { counts_busy: countsBusy }) });
        toast.success(teamId ? "Team calendar added" : "Calendar added");
      }
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the calendar. Try again."));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={calendar ? "Calendar settings" : "New calendar"}
      size="sm"
      footer={
        <div className="flex w-full justify-end gap-2">
          <Button variant="ghost" size="auto" onClick={onClose}>
            Cancel
          </Button>
          <Button size="auto" loading={create.isPending || update.isPending} onClick={() => void submit()}>
            {calendar ? "Save" : "Add calendar"}
          </Button>
        </div>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Name" error={error ?? undefined} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} autoFocus placeholder="Work, Personal, Hiring…" />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={1000} minRows={2} />
        </Field>
        {!calendar ? (
          <>
            {orgs.length > 1 ? (
              <Field label={orgsNoun().replace(/^./, (c) => c.toUpperCase())} info={`Sharing with teams and everyone works inside one ${orgsNoun()}.`}>
                <Select value={orgId} onValueChange={(v) => { setOrgId(v); setTeamId(""); }} options={orgs.map((o) => ({ value: o.id, label: o.name }))} />
              </Field>
            ) : null}
            <Field label="For a team" info="Everyone on the team can add and change its events. Team events don't block anyone's availability.">
              <Select
                value={teamId || "none"}
                onValueChange={(v) => setTeamId(v === "none" ? "" : v)}
                options={[{ value: "none", label: "Just me" }, ...(teams.data ?? []).map((t) => ({ value: t.id, label: t.name }))]}
              />
            </Field>
          </>
        ) : null}
        {!teamId ? (
          <Switch checked={countsBusy} onCheckedChange={setCountsBusy} label="Its events make me busy" />
        ) : null}
        <Field label="Color">
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Color">
            {CALENDAR_COLORS.map((c) => (
              <button
                key={c}
                type="button"
                role="radio"
                aria-checked={color === c}
                aria-label={CALENDAR_COLOR_NAMES[c]}
                onClick={() => setColor(c)}
                className={cn(
                  "flex size-10 items-center justify-center rounded-pill focus-visible:outline-2 focus-visible:outline-ring",
                  CALENDAR_COLOR_CLASSES[c].chip,
                  color === c && "ring-2 ring-offset-2 ring-offset-surface " + CALENDAR_COLOR_CLASSES[c].ring,
                )}
              >
                {color === c ? <Check className="size-4" aria-hidden /> : null}
              </button>
            ))}
          </div>
        </Field>
      </form>
    </Dialog>
  );
}

/** Calendars shared with you that aren't in your list, each with Add. */
function SharedCalendarsDialog({
  open,
  onOpenChange,
  calendars,
  loading,
  error,
  onRetry,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  calendars: CalendarCalendar[];
  loading: boolean;
  error: string | null;
  onRetry: () => void;
}) {
  const prefs = useCalendarCalendarPrefs();
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Shared with you" info="Calendars colleagues and teams shared with you. Add one to see it beside yours." size="md">
      {loading ? (
        <div className="flex flex-col gap-2" aria-busy>
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
        </div>
      ) : error ? (
        <ErrorState title="Couldn't load calendars" info={error} onRetry={onRetry} />
      ) : calendars.length === 0 ? (
        <EmptyState title="Nothing else shared with you" info="When someone shares a calendar with you, it shows up here." />
      ) : (
        <ul className="flex flex-col divide-y divide-border">
          {calendars.map((c) => (
            <li key={c.id} className="flex min-h-12 items-center gap-3 py-2">
              <span aria-hidden className={cn("size-3 shrink-0 rounded-pill", CALENDAR_COLOR_CLASSES[c.color].dot)} />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate type-body text-fg">{c.name}</span>
                <span className="truncate type-caption text-fg-muted">
                  {c.team_name ? `Team: ${c.team_name}` : (c.owner?.display_name ?? "")} · {CALENDAR_SHARE_LEVEL_LABEL[c.level]}
                </span>
              </span>
              <Button
                size="sm"
                variant="secondary"
                leftIcon={<Plus />}
                loading={prefs.isPending && prefs.variables?.id === c.id}
                onClick={() =>
                  prefs.mutate({ id: c.id, added: true, hidden: false }, { onSuccess: () => toast.success(`${c.name} added`), onError: (e) => toast.error(errorMessage(e, "Couldn't add it. Try again.")) })
                }
              >
                Add
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}
