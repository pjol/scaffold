"use client";

import {
  CRM_ACTIVITY_LABEL,
  CRM_CALL_OUTCOMES,
  CRM_MEETING_OUTCOMES,
  CRM_TYPE_LABEL,
  accessLevelAtLeast,
  crmBodySegments,
  crmDuration,
  crmMentionToken,
  crmOutcomeLabel,
  crmUpcoming,
  errorMessage,
  useAccessDirectory,
  useCrmDeleteActivity,
  useCrmLogActivity,
  useCrmTimeline,
  useCrmUpdateActivity,
  type AccessDirectoryEntry,
  type CrmActivity,
  type CrmActivityRequest,
  type CrmActivityType,
  type CrmDirection,
  type CrmEvent,
  type CrmRecord,
  type CrmRecordRef,
  type CrmTimelineFilter,
  type CrmTimelineItem,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  Select,
  Skeleton,
  SkeletonGroup,
  SourceBadge,
  TextArea,
  cn,
  sourcePlatformName,
  toast,
} from "@scaffoldapp/ui-web";
import {
  ArrowRightLeft,
  CheckSquare,
  GitMerge,
  History,
  Link2,
  Mail,
  MoreHorizontal,
  Pencil,
  Phone,
  PlusCircle,
  StickyNote,
  Trash2,
  Unlink,
  Upload,
  Users,
  X,
} from "lucide-react";
import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { useCrm } from "./CrmFrame";
import { TaskFields, TaskRow, emptyTask, taskRequest, type TaskDraft } from "./TasksScreen";
import { recordHref } from "./parts";

const ACTIVITY_ICON: Record<CrmActivityType, typeof StickyNote> = { note: StickyNote, call: Phone, email: Mail, meeting: Users, task: CheckSquare };

const FILTERS: { value: CrmTimelineFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "notes", label: "Notes" },
  { value: "calls", label: "Calls" },
  { value: "emails", label: "Emails" },
  { value: "meetings", label: "Meetings" },
  { value: "tasks", label: "Tasks" },
  { value: "changes", label: "Changes" },
];

/** The record's timeline with the composer on top and open tasks first. */
export function TimelinePanel({ record, suggestions }: { record: CrmRecord; suggestions: CrmRecordRef[] }) {
  const [filter, setFilter] = useUrlState<CrmTimelineFilter>("activity", "all");
  // Platforms keep their own field history; a connected record's timeline is its engagements.
  const filters = record.source ? FILTERS.filter((f) => f.value !== "changes") : FILTERS;
  const timeline = useCrmTimeline(record.type, record.id, filter);
  const upcoming = crmUpcoming(timeline.data);
  const canComment = accessLevelAtLeast(record.my_level === "none" ? null : record.my_level, "comment");
  return (
    <div className="flex flex-col gap-4">
      {canComment ? <Composer record={record} suggestions={suggestions} /> : null}
      {upcoming.length > 0 ? (
        <section className="flex flex-col gap-2" aria-label="Open tasks">
          <h3 className="type-label font-semibold text-fg">Open tasks</h3>
          <Card padding="none" className="p-1">
            <ul className="flex flex-col">
              {upcoming.map((t) => (
                <TaskRow key={t.id} task={t} showAssignee />
              ))}
            </ul>
          </Card>
        </section>
      ) : null}
      <div className="-mx-1 flex gap-1 overflow-x-auto px-1" role="tablist" aria-label="Show">
        {filters.map((f) => (
          <button
            key={f.value}
            type="button"
            role="tab"
            aria-selected={filter === f.value}
            onClick={() => setFilter(f.value)}
            className={cn(
              "inline-flex h-8 shrink-0 items-center rounded-md px-3 type-label font-semibold transition-colors",
              filter === f.value ? "bg-sunken text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
            )}
          >
            {f.label}
          </button>
        ))}
      </div>
      {timeline.isPending ? (
        <SkeletonGroup label="Loading the timeline" className="flex flex-col gap-3">
          {[0, 1, 2].map((i) => (
            <Card key={i} padding="compact" className="flex gap-3">
              <Skeleton shape="circle" className="size-8 shrink-0" />
              <div className="flex flex-1 flex-col gap-2">
                <Skeleton shape="text" className="h-4 w-1/2" />
                <Skeleton shape="text" className="h-4 w-full" />
              </div>
            </Card>
          ))}
        </SkeletonGroup>
      ) : timeline.isError ? (
        <ErrorState compact title="Couldn't load the timeline" info={timeline.error.message} onRetry={() => void timeline.refetch()} retrying={timeline.isRefetching} />
      ) : timeline.data.items.length === 0 ? (
        <EmptyState compact icon={<History />} title={filter === "all" ? "Nothing here yet" : `No ${FILTERS.find((f) => f.value === filter)?.label.toLowerCase()} yet`} />
      ) : (
        <ol className="flex flex-col gap-3" aria-label="Timeline">
          {timeline.data.items.map((it) => (
            <TimelineEntry key={it.id} item={it} record={record} />
          ))}
        </ol>
      )}
      <LoadMore hasNextPage={!!timeline.hasNextPage} isFetchingNextPage={timeline.isFetchingNextPage} fetchNextPage={timeline.fetchNextPage} />
    </div>
  );
}

function TimelineEntry({ item, record }: { item: CrmTimelineItem; record: CrmRecord }) {
  if (item.activity) return <ActivityCard activity={item.activity} record={record} />;
  if (item.event) return <EventRow event={item.event} at={item.at} record={record} />;
  return null;
}

function who(a: { created_by?: CrmActivity["created_by"]; agent: string | null; actor?: CrmEvent["actor"] }) {
  const person = a.created_by?.name ?? a.actor?.name ?? "Someone";
  return a.agent ? `${person} (via ${a.agent})` : person;
}

function ActivityCard({ activity: a, record }: { activity: CrmActivity; record: CrmRecord }) {
  const Icon = ACTIVITY_ICON[a.type];
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const del = useCrmDeleteActivity();
  const others = a.records.filter((r) => !(r.type === record.type && r.id === record.id));
  const verb = a.type === "note" ? "left a note" : a.type === "task" ? "created a task" : `logged ${a.type === "email" ? "an email" : `a ${a.type}`}`;
  return (
    <li>
      <Card padding="compact" className="flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <span className="flex size-8 shrink-0 items-center justify-center rounded-pill bg-sunken text-fg-muted" aria-hidden>
            <Icon className="size-4" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="type-label text-fg">
              <span className="font-semibold">{who(a)}</span> {verb}
            </span>
            <time className="type-caption text-fg-muted" dateTime={a.occurred_at} title={formatDateTime(a.occurred_at)}>
              {a.type === "task" ? formatRelative(a.created_at) : formatDateTime(a.occurred_at)}
            </time>
          </div>
          <SourceBadge source={a.source} iconOnly />
          {a.can_edit && !a.source ? (
            <Menu trigger={<IconButton label="Activity actions" icon={<MoreHorizontal />} size="sm" />}>
              <MenuItem icon={<Pencil />} onSelect={() => setEditing(true)}>
                Edit
              </MenuItem>
              <MenuSeparator />
              <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(true)}>
                Delete
              </MenuItem>
            </Menu>
          ) : null}
        </div>
        {editing ? (
          <EditActivity activity={a} onDone={() => setEditing(false)} />
        ) : (
          <div className="flex flex-col gap-2 pl-11">
            {a.type === "task" ? (
              <ul>
                <TaskRow task={a} showAssignee compact />
              </ul>
            ) : (
              <>
                {a.outcome || a.direction || a.duration_minutes ? (
                  <div className="flex flex-wrap gap-1.5">
                    {a.direction ? <Badge>{a.direction === "inbound" ? (a.type === "email" ? "Received" : "Inbound") : a.type === "email" ? "Sent" : "Outbound"}</Badge> : null}
                    {a.outcome ? <Badge tone={a.outcome === "connected" || a.outcome === "completed" ? "success" : "neutral"}>{crmOutcomeLabel(a.outcome)}</Badge> : null}
                    {a.duration_minutes ? <Badge>{a.duration_minutes} min</Badge> : null}
                  </div>
                ) : null}
                {a.subject ? <p className="type-label font-semibold text-fg">{a.subject}</p> : null}
                {a.body ? <Body body={a.body} activity={a} /> : null}
              </>
            )}
            {others.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5 type-caption text-fg-muted">
                <Link2 className="size-3.5" aria-hidden />
                Also on
                {others.map((r) => (
                  <Link key={`${r.type}:${r.id}`} href={recordHref(r.type, r.id)} className="font-semibold text-fg hover:underline">
                    {r.name}
                  </Link>
                ))}
              </div>
            ) : null}
          </div>
        )}
      </Card>
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete this ${CRM_ACTIVITY_LABEL[a.type].toLowerCase()}?`}
        confirmLabel="Delete"
        onConfirm={() => del.mutateAsync(a).then(() => toast.success("Deleted"), (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))))}
      >
        It goes from every record it&apos;s on. It can&apos;t be undone.
      </ConfirmDialog>
    </li>
  );
}

function Body({ body, activity }: { body: string; activity: CrmActivity }) {
  return (
    <p className="whitespace-pre-wrap break-words type-body text-fg">
      {crmBodySegments(body, activity.mentions).map((s, i) =>
        s.kind === "text" ? (
          <span key={i}>{s.text}</span>
        ) : (
          <span key={i} className="rounded-xs bg-primary-soft px-0.5 font-semibold text-primary-strong">
            @{s.mention?.name ?? "someone"}
          </span>
        ),
      )}
    </p>
  );
}

function EventRow({ event: e, at, record }: { event: CrmEvent; at: string; record: CrmRecord }) {
  const actor = who({ actor: e.actor, agent: e.agent });
  const d = e.data;
  let icon = <History className="size-4" aria-hidden />;
  let text: React.ReactNode = null;
  switch (e.kind) {
    case "created":
      icon = <PlusCircle className="size-4" aria-hidden />;
      text = (
        <>
          <b className="font-semibold">{actor}</b> created this {record.type}
        </>
      );
      break;
    case "imported":
      icon = <Upload className="size-4" aria-hidden />;
      text = (
        <>
          <b className="font-semibold">{actor}</b> imported this {record.type}
        </>
      );
      break;
    case "merged":
      icon = <GitMerge className="size-4" aria-hidden />;
      text = (
        <>
          <b className="font-semibold">{actor}</b> merged “{d.merged_name}” into this {record.type}
        </>
      );
      break;
    case "stage_changed":
      icon = <ArrowRightLeft className="size-4" aria-hidden />;
      text = (
        <>
          <b className="font-semibold">{actor}</b> moved it from {d.from_stage} to <b className="font-semibold">{d.to_stage}</b>
          {d.to_pipeline ? ` (${d.to_pipeline})` : ""}
          {d.seconds_in_stage ? <span className="text-fg-muted"> · {crmDuration(d.seconds_in_stage)} in {d.from_stage}</span> : null}
        </>
      );
      break;
    case "associated":
    case "dissociated": {
      icon = e.kind === "associated" ? <Link2 className="size-4" aria-hidden /> : <Unlink className="size-4" aria-hidden />;
      const kind = d.other_type ? CRM_TYPE_LABEL[d.other_type].one : "record";
      const name =
        d.other_hidden || !d.other_id || !d.other_type ? (
          <span className="text-fg-muted">a {kind} you can&apos;t see</span>
        ) : (
          <Link href={recordHref(d.other_type, d.other_id)} className="font-semibold hover:underline">
            {d.other_name}
          </Link>
        );
      text = (
        <>
          <b className="font-semibold">{actor}</b> {e.kind === "associated" ? "linked" : "unlinked"} {d.other_hidden ? "" : `${kind} `}
          {name}
        </>
      );
      break;
    }
    case "updated":
      text = (
        <>
          <b className="font-semibold">{actor}</b> changed{" "}
          {(d.changes ?? []).map((c, i) => (
            <span key={c.key}>
              {i > 0 ? ", " : ""}
              {c.label}
              {c.after ? (
                <>
                  {" "}
                  to <b className="font-semibold">{c.after}</b>
                </>
              ) : (
                " (cleared)"
              )}
            </span>
          ))}
        </>
      );
      break;
  }
  return (
    <li className="flex items-start gap-3 px-3">
      <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center text-fg-subtle">{icon}</span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 py-1.5">
        <span className="type-label text-fg">{text}</span>
        {e.kind === "updated" && (d.changes ?? []).some((c) => c.before) ? (
          <span className="type-caption text-fg-muted">
            Was:{" "}
            {(d.changes ?? [])
              .filter((c) => c.before)
              .map((c) => `${c.label} ${c.before}`)
              .join(" · ")}
          </span>
        ) : null}
        <time className="type-caption text-fg-subtle" dateTime={at} title={formatDateTime(at)}>
          {formatRelative(at)}
        </time>
      </div>
    </li>
  );
}

// ---- composer --------------------------------------------------------------------

type Kind = "note" | "call" | "email" | "meeting" | "task";

const KIND_OPTIONS: { value: Kind; label: string }[] = [
  { value: "note", label: "Note" },
  { value: "call", label: "Call" },
  { value: "email", label: "Email" },
  { value: "meeting", label: "Meeting" },
  { value: "task", label: "Task" },
];

function nowLocal(): string {
  const d = new Date();
  d.setSeconds(0, 0);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Log a note, call, email or meeting, or create a task, with @mentions and the records it's also about. */
function Composer({ record, suggestions }: { record: CrmRecord; suggestions: CrmRecordRef[] }) {
  const { orgId } = useCrm();
  // Connected records take notes, calls and tasks, written to the platform; people are its owners.
  const external = record.source?.type === "connected" ? record.source : null;
  const kinds = external ? KIND_OPTIONS.filter((o) => o.value === "note" || o.value === "call" || o.value === "task") : KIND_OPTIONS;
  const [kind, setKind] = useState<Kind>("note");
  const [text, setText] = useState("");
  const [mentions, setMentions] = useState<AccessDirectoryEntry[]>([]);
  const [subject, setSubject] = useState("");
  const [outcome, setOutcome] = useState("");
  const [direction, setDirection] = useState<CrmDirection>("outbound");
  const [minutes, setMinutes] = useState("");
  const [when, setWhen] = useState(nowLocal);
  const [task, setTask] = useState<TaskDraft>(emptyTask);
  const [alsoOn, setAlsoOn] = useState<CrmRecordRef[]>(suggestions);
  const [error, setError] = useState<string | null>(null);
  const log = useCrmLogActivity(record.type, record.id);
  const suggestionKey = suggestions.map((s) => s.id).join(",");
  const [seen, setSeen] = useState(suggestionKey);
  if (seen !== suggestionKey) {
    setSeen(suggestionKey);
    setAlsoOn(suggestions);
  }

  const reset = () => {
    setText("");
    setMentions([]);
    setSubject("");
    setOutcome("");
    setMinutes("");
    setWhen(nowLocal());
    setTask(emptyTask());
    setError(null);
  };
  const submit = () => {
    const body = encodeMentions(text, mentions);
    let req: CrmActivityRequest;
    if (kind === "task") {
      if (!task.subject.trim()) return setError("Give the task a title.");
      req = { ...taskRequest(task), ...(body.trim() ? { body } : {}) };
    } else {
      if (kind === "note" && !body.trim()) return setError("Write the note first.");
      if (kind !== "note" && !body.trim() && !subject.trim() && !outcome) return setError("Add what happened before logging it.");
      req = {
        type: kind,
        body,
        ...(subject.trim() ? { subject: subject.trim() } : {}),
        ...(kind !== "note" && when ? { occurred_at: new Date(when).toISOString() } : {}),
        ...((kind === "call" || kind === "meeting") && outcome ? { outcome } : {}),
        ...(kind === "call" || kind === "email" ? { direction } : {}),
        ...((kind === "call" || kind === "meeting") && minutes ? { duration_minutes: Number(minutes) } : {}),
      };
    }
    if (!external) req.also_on = alsoOn.map((r) => ({ type: r.type, id: r.id }));
    setError(null);
    log.mutate(req, {
      onSuccess: (a) => {
        const where = external ? ` in ${sourcePlatformName(external.provider)}` : "";
        toast.success(kind === "task" ? `Task added${where}` : `${CRM_ACTIVITY_LABEL[kind]} logged${where}`);
        if (a.unreachable?.length) toast.info(`${a.unreachable.join(", ")} can't see this record, so wasn't notified.`);
        reset();
      },
      onError: (e) => setError(errorMessage(e, "Couldn't save it.")),
    });
  };
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-1" role="radiogroup" aria-label="What to log">
        {kinds.map((o) => {
          const Icon = ACTIVITY_ICON[o.value];
          return (
            <button
              key={o.value}
              type="button"
              role="radio"
              aria-checked={kind === o.value}
              onClick={() => setKind(o.value)}
              className={cn(
                "inline-flex h-control items-center gap-1.5 rounded-md px-3 type-label font-semibold transition-colors",
                kind === o.value ? "bg-primary-soft text-primary-strong" : "text-fg-muted hover:bg-sunken hover:text-fg",
              )}
            >
              <Icon className="size-4" aria-hidden />
              {o.label}
            </button>
          );
        })}
      </div>
      {kind === "task" ? (
        <TaskFields orgId={orgId} value={task} onChange={setTask} source={external?.connection_id} />
      ) : (
        <>
          {kind !== "note" ? (
            <div className="grid gap-3 sm:grid-cols-2">
              {kind === "email" || kind === "meeting" ? (
                <Field label={kind === "email" ? "Subject" : "Title"}>
                  <Input value={subject} maxLength={200} onChange={(e) => setSubject(e.target.value)} />
                </Field>
              ) : null}
              <Field label="When">
                <Input type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
              </Field>
              {kind === "call" || kind === "meeting" ? (
                <Field label="Outcome">
                  <Select value={outcome || undefined} onValueChange={setOutcome} placeholder="Choose" options={kind === "call" ? CRM_CALL_OUTCOMES : CRM_MEETING_OUTCOMES} />
                </Field>
              ) : null}
              {kind === "call" || kind === "email" ? (
                <Field label="Direction">
                  <Select
                    value={direction}
                    onValueChange={(v) => setDirection(v as CrmDirection)}
                    options={kind === "email" ? [{ value: "outbound", label: "Sent" }, { value: "inbound", label: "Received" }] : [{ value: "outbound", label: "Outbound" }, { value: "inbound", label: "Inbound" }]}
                  />
                </Field>
              ) : null}
              {kind === "call" || kind === "meeting" ? (
                <Field label="Minutes">
                  <Input type="number" min={0} max={1440} value={minutes} onChange={(e) => setMinutes(e.target.value)} />
                </Field>
              ) : null}
            </div>
          ) : null}
        </>
      )}
      {external ? (
        <TextArea aria-label={kind === "task" ? "Notes" : kind === "note" ? "Note" : "What happened"} placeholder={kind === "note" ? "Note" : "What happened"} value={text} onChange={(e) => setText(e.target.value)} />
      ) : (
        <MentionInput orgId={orgId} label={kind === "task" ? "Notes" : kind === "note" ? "Note" : "What happened"} value={text} onChange={setText} mentions={mentions} onMention={(m) => setMentions((x) => (x.some((y) => y.id === m.id) ? x : [...x, m]))} />
      )}
      {alsoOn.length > 0 && !external ? (
        <div className="flex flex-wrap items-center gap-2 type-caption text-fg-muted">
          <span>Also log on</span>
          {alsoOn.map((r) => (
            <span key={r.id} className="inline-flex h-7 items-center gap-1 rounded-md border border-border pl-2 pr-0.5 type-caption text-fg">
              {r.name}
              <IconButton label={`Don't log on ${r.name}`} size="sm" icon={<X />} onClick={() => setAlsoOn(alsoOn.filter((x) => x.id !== r.id))} />
            </span>
          ))}
        </div>
      ) : null}
      {error ? (
        <p role="alert" className="type-label text-danger-strong">
          {error}
        </p>
      ) : null}
      <div className="flex justify-end">
        <Button loading={log.isPending} onClick={submit}>
          {kind === "task" ? "Add task" : kind === "note" ? "Save note" : `Log ${CRM_ACTIVITY_LABEL[kind].toLowerCase()}`}
        </Button>
      </div>
    </Card>
  );
}

/** Turn "@Name" written through the picker into mention tokens. */
function encodeMentions(text: string, mentions: AccessDirectoryEntry[]): string {
  let out = text;
  for (const m of [...mentions].sort((a, b) => b.name.length - a.name.length)) {
    out = out.split(`@${m.name}`).join(crmMentionToken({ id: m.id, type: m.type }));
  }
  return out;
}

/** Text area with @mention suggestions from the org's people and teams. */
function MentionInput({
  orgId,
  label,
  value,
  onChange,
  mentions,
  onMention,
}: {
  orgId: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  mentions: AccessDirectoryEntry[];
  onMention: (m: AccessDirectoryEntry) => void;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const [query, setQuery] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const dir = useAccessDirectory(orgId, query ?? "", { limit: 6, enabled: query !== null });
  const items = dir.data ?? [];
  const detect = (v: string, caret: number) => {
    const before = v.slice(0, caret);
    const m = /(^|\s)@([\p{L}\p{N}._-]{0,30})$/u.exec(before);
    setQuery(m ? (m[2] ?? "") : null);
    setActive(0);
  };
  const pick = (e: AccessDirectoryEntry) => {
    const el = ref.current;
    const caret = el?.selectionStart ?? value.length;
    const before = value.slice(0, caret).replace(/@([\p{L}\p{N}._-]{0,30})$/u, `@${e.name} `);
    const next = before + value.slice(caret);
    onChange(next);
    onMention(e);
    setQuery(null);
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(before.length, before.length);
    });
  };
  const used = useMemo(() => mentions.filter((m) => value.includes(`@${m.name}`)), [mentions, value]);
  return (
    <div className="relative flex flex-col gap-2">
      <TextArea
        ref={ref}
        aria-label={label}
        placeholder={`${label}. Type @ to mention someone.`}
        value={value}
        minRows={3}
        onChange={(e) => {
          onChange(e.target.value);
          detect(e.target.value, e.target.selectionStart);
        }}
        onKeyDown={(e) => {
          if (query === null || items.length === 0) return;
          if (e.key === "ArrowDown") {
            e.preventDefault();
            setActive((a) => Math.min(a + 1, items.length - 1));
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            setActive((a) => Math.max(a - 1, 0));
          } else if (e.key === "Enter" || e.key === "Tab") {
            e.preventDefault();
            const it = items[active];
            if (it) pick(it);
          } else if (e.key === "Escape") {
            setQuery(null);
          }
        }}
        onBlur={() => setTimeout(() => setQuery(null), 150)}
      />
      {query !== null && items.length > 0 ? (
        <ul role="listbox" aria-label="Mention" className="flex flex-col rounded-md border border-border bg-surface-raised p-1 shadow-md">
          {items.map((it, i) => (
            <li
              key={it.id}
              role="option"
              aria-selected={i === active}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => pick(it)}
              className={cn("flex min-h-control cursor-pointer items-center gap-2 rounded-sm px-2", i === active && "bg-sunken")}
            >
              <AppAvatar name={it.name} src={it.avatar_url} size="xs" />
              <span className="type-label text-fg">{it.name}</span>
              {it.type === "team" ? <span className="type-caption text-fg-muted">@{it.handle}</span> : null}
            </li>
          ))}
        </ul>
      ) : null}
      {used.length > 0 ? <span className="type-caption text-fg-muted">Notifies {used.map((m) => m.name).join(", ")}</span> : null}
    </div>
  );
}

function EditActivity({ activity: a, onDone }: { activity: CrmActivity; onDone: () => void }) {
  const update = useCrmUpdateActivity(a.org_id);
  const [subject, setSubject] = useState(a.subject);
  const [body, setBody] = useState(a.body);
  const save = () =>
    update.mutate(
      { id: a.id, ...(a.type !== "note" ? { subject } : {}), body },
      {
        onSuccess: () => {
          toast.success("Saved");
          onDone();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save it.")),
      },
    );
  return (
    <div className="flex flex-col gap-3 pl-11">
      {a.type !== "note" ? (
        <Field label={a.type === "task" ? "Title" : "Subject"}>
          <Input value={subject} onChange={(e) => setSubject(e.target.value)} maxLength={200} />
        </Field>
      ) : null}
      <Field label="Text" info="Mentions show as tokens while editing.">
        <TextArea value={body} onChange={(e) => setBody(e.target.value)} />
      </Field>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onDone}>
          Cancel
        </Button>
        <Button size="sm" loading={update.isPending} onClick={save}>
          Save
        </Button>
      </div>
    </div>
  );
}
