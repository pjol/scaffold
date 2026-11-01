"use client";

import {
  CRM_PRIORITY_LABEL,
  CRM_TYPE_LABEL,
  crmIsOverdue,
  errorMessage,
  useCrmLogActivity,
  useCrmTaskSummary,
  useCrmTasks,
  useCrmUpdateActivity,
  useMe,
  type CrmActivity,
  type CrmPriority,
  type CrmSearchHit,
  type CrmTaskBucket,
  type CrmUserRef,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, Dialog, EmptyState, ErrorState, Field, Input, SegmentedControl, Select, SkeletonGroup, SkeletonRow, Tab, Tabs, TabsList, cn, toast } from "@scaffoldapp/ui-web";
import { CheckSquare, Plus, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { MemberPicker, PersonChip, RecordPicker, TYPE_ICON, dayLabel, recordHref, timeLabel } from "./parts";

const BUCKETS: { value: CrmTaskBucket; label: string }[] = [
  { value: "overdue", label: "Overdue" },
  { value: "today", label: "Today" },
  { value: "upcoming", label: "Upcoming" },
  { value: "done", label: "Done" },
];

/** /crm/tasks: your (or everyone's) tasks by when they're due, with New task. */
export function CrmTasksScreen() {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame
      actions={
        <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
          New task
        </Button>
      }
    >
      <Tasks />
      <NewTaskDialog open={creating} onOpenChange={setCreating} />
    </CrmFrame>
  );
}

function Tasks() {
  const { orgId } = useCrm();
  const [bucket, setBucket] = useUrlState<CrmTaskBucket>("bucket", "today");
  const [who, setWho] = useUrlState<"me" | "any">("who", "me");
  const summary = useCrmTaskSummary(orgId);
  const tasks = useCrmTasks(orgId, bucket, who);
  const counts: Partial<Record<CrmTaskBucket, number>> = summary.data ? { overdue: summary.data.overdue, today: summary.data.today, upcoming: summary.data.upcoming } : {};
  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="-mx-1 overflow-x-auto px-1">
          <Tabs value={bucket} onValueChange={(v) => setBucket(v as CrmTaskBucket)} variant="pill">
            <TabsList aria-label="When" className="w-max">
              {BUCKETS.map((b) => (
                <Tab key={b.value} value={b.value} {...(who === "me" && counts[b.value] ? { count: counts[b.value] } : {})}>
                  {b.label}
                </Tab>
              ))}
            </TabsList>
          </Tabs>
        </div>
        <SegmentedControl<"me" | "any">
          aria-label="Assigned to"
          className="sm:ml-auto sm:w-56"
          value={who}
          onValueChange={setWho}
          options={[
            { value: "me", label: "Mine" },
            { value: "any", label: "Everyone" },
          ]}
        />
      </div>
      <Card padding="none" className="p-1">
        {tasks.isPending ? (
          <SkeletonGroup label="Loading tasks" className="flex flex-col">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : tasks.isError ? (
          <ErrorState compact title="Couldn't load tasks" info={tasks.error.message} onRetry={() => void tasks.refetch()} retrying={tasks.isRefetching} />
        ) : tasks.data.items.length === 0 ? (
          <EmptyState compact icon={<CheckSquare />} title={bucket === "done" ? "No finished tasks yet" : bucket === "overdue" ? "Nothing overdue" : bucket === "today" ? "Nothing due today" : "No upcoming tasks"} />
        ) : (
          <ul className="flex flex-col">
            {tasks.data.items.map((t) => (
              <TaskRow key={t.id} task={t} showAssignee={who === "any"} />
            ))}
          </ul>
        )}
        <LoadMore hasNextPage={!!tasks.hasNextPage} isFetchingNextPage={tasks.isFetchingNextPage} fetchNextPage={tasks.fetchNextPage} />
      </Card>
    </section>
  );
}

/** One task: done checkbox, title, record, due time (late in danger), assignee. */
export function TaskRow({ task, showAssignee, compact }: { task: CrmActivity; showAssignee?: boolean; compact?: boolean }) {
  const update = useCrmUpdateActivity(task.org_id);
  const done = !!task.done_at;
  const late = crmIsOverdue(task);
  const rec = task.records[0];
  const toggle = (v: boolean) =>
    update.mutate(
      { id: task.id, done: v },
      {
        onSuccess: () => toast.success(v ? "Task done" : "Task reopened"),
        onError: (e) => toast.error(errorMessage(e, "Couldn't update the task.")),
      },
    );
  return (
    <li className={cn("flex items-start gap-3 rounded-sm px-3 py-2.5", !compact && "min-h-tap")}>
      <Checkbox
        aria-label={done ? `Reopen “${task.subject}”` : `Mark “${task.subject}” done`}
        checked={done}
        disabled={update.isPending}
        onCheckedChange={toggle}
        className="mt-0.5"
      />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className={cn("type-label font-semibold", done ? "text-fg-muted line-through" : "text-fg")}>{task.subject}</span>
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1 type-caption text-fg-muted">
          {rec ? (
            <Link href={recordHref(rec.type, rec.id)} className="inline-flex min-w-0 items-center gap-1 hover:text-fg hover:underline">
              {(() => {
                const Icon = TYPE_ICON[rec.type];
                return <Icon className="size-3.5 shrink-0" aria-hidden />;
              })()}
              <span className="truncate">{rec.name}</span>
            </Link>
          ) : null}
          {task.due_at ? (
            <span className={cn("tabular-nums", late && "font-semibold text-danger-strong")}>
              {dayLabel(task.due_at)} · {timeLabel(task.due_at)}
            </span>
          ) : (
            <span>No due date</span>
          )}
          {task.priority !== "none" ? <Badge tone={task.priority === "high" ? "danger" : task.priority === "medium" ? "warning" : "neutral"}>{CRM_PRIORITY_LABEL[task.priority]}</Badge> : null}
        </span>
      </div>
      {showAssignee && task.assignee ? (
        <span className="hidden shrink-0 type-caption text-fg-muted sm:inline-flex">
          <PersonChip user={task.assignee} />
        </span>
      ) : null}
    </li>
  );
}

const REMINDERS: { value: string; label: string; minutes: number | null }[] = [
  { value: "none", label: "No reminder", minutes: null },
  { value: "0", label: "At the due time", minutes: 0 },
  { value: "15", label: "15 minutes before", minutes: 15 },
  { value: "60", label: "1 hour before", minutes: 60 },
  { value: "1440", label: "1 day before", minutes: 1440 },
];

/** The fields of a new task, shared by the tasks screen and the record composer. */
export function TaskFields({
  orgId,
  value,
  onChange,
}: {
  orgId: string;
  value: TaskDraft;
  onChange: (v: TaskDraft) => void;
}) {
  const [picking, setPicking] = useState(false);
  return (
    <div className="flex flex-col gap-4">
      <Field label="Title" required>
        <Input value={value.subject} maxLength={200} onChange={(e) => onChange({ ...value, subject: e.target.value })} placeholder="Call back about pricing" />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Due date">
          <Input type="date" value={value.date} onChange={(e) => onChange({ ...value, date: e.target.value })} />
        </Field>
        <Field label="Time">
          <Input type="time" value={value.time} onChange={(e) => onChange({ ...value, time: e.target.value })} />
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Reminder" info="Sent to the assignee as a notification, once.">
          <Select value={value.reminder} onValueChange={(v) => onChange({ ...value, reminder: v })} options={REMINDERS.map((r) => ({ value: r.value, label: r.label }))} />
        </Field>
        <Field label="Priority">
          <Select
            value={value.priority}
            onValueChange={(v) => onChange({ ...value, priority: v as CrmPriority })}
            options={(Object.keys(CRM_PRIORITY_LABEL) as CrmPriority[]).map((p) => ({ value: p, label: CRM_PRIORITY_LABEL[p] }))}
          />
        </Field>
      </div>
      <Field label="Assignee" info="They must be able to see the record.">
        {picking ? (
          <MemberPicker
            orgId={orgId}
            label="Choose the assignee"
            autoFocus
            onPick={(u) => {
              onChange({ ...value, assignee: u });
              setPicking(false);
            }}
          />
        ) : (
          <div className="flex min-h-control items-center gap-2">
            {value.assignee ? <PersonChip user={value.assignee} size="sm" /> : <span className="type-label text-fg-muted">You</span>}
            <Button variant="ghost" size="sm" onClick={() => setPicking(true)}>
              Change
            </Button>
          </div>
        )}
      </Field>
    </div>
  );
}

export interface TaskDraft {
  subject: string;
  date: string;
  time: string;
  reminder: string;
  priority: CrmPriority;
  assignee: CrmUserRef | null;
}

export function emptyTask(): TaskDraft {
  const d = new Date(Date.now() + 86_400_000);
  const pad = (n: number) => String(n).padStart(2, "0");
  return { subject: "", date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`, time: "09:00", reminder: "15", priority: "none", assignee: null };
}

/** The API fields of a task draft. */
export function taskRequest(t: TaskDraft) {
  const due = t.date ? new Date(`${t.date}T${t.time || "09:00"}`) : null;
  const r = REMINDERS.find((x) => x.value === t.reminder);
  return {
    type: "task" as const,
    subject: t.subject.trim(),
    priority: t.priority,
    ...(due ? { due_at: due.toISOString() } : {}),
    ...(due && r?.minutes !== null && r?.minutes !== undefined ? { reminder_at: new Date(due.getTime() - r.minutes * 60_000).toISOString() } : {}),
    ...(t.assignee ? { assignee_id: t.assignee.id } : {}),
  };
}

function NewTaskDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const { orgId } = useCrm();
  const [record, setRecord] = useState<CrmSearchHit | null>(null);
  const [draft, setDraft] = useState<TaskDraft>(emptyTask);
  const [error, setError] = useState<string | null>(null);
  const log = useCrmLogActivity(record?.type ?? "contact", record?.id ?? "");
  const me = useMe();
  const submit = () => {
    if (!record) return setError("Choose the record the task is about.");
    if (!draft.subject.trim()) return setError("Give the task a title.");
    setError(null);
    log.mutate(taskRequest(draft), {
      onSuccess: () => {
        toast.success(draft.assignee && draft.assignee.id !== me.data?.id ? `Task assigned to ${draft.assignee.name}` : "Task added");
        setDraft(emptyTask());
        setRecord(null);
        onOpenChange(false);
      },
      onError: (e) => setError(errorMessage(e, "Couldn't add the task.")),
    });
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New task"
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={log.isPending} onClick={submit}>
            Add task
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Record" required error={error}>
          {record ? (
            <div className="flex min-h-control items-center gap-2 rounded-md border border-border px-3">
              {(() => {
                const Icon = TYPE_ICON[record.type];
                return <Icon className="size-4 text-fg-muted" aria-hidden />;
              })()}
              <span className="min-w-0 flex-1 truncate type-label text-fg">{record.title}</span>
              <span className="type-caption text-fg-muted">{CRM_TYPE_LABEL[record.type].One}</span>
              <Button variant="ghost" size="sm" aria-label="Choose another record" onClick={() => setRecord(null)} leftIcon={<X />}>
                Change
              </Button>
            </div>
          ) : (
            <RecordPicker orgId={orgId} types={["contact", "company", "deal"]} label="Find a contact, company or deal" onPick={setRecord} autoFocus />
          )}
        </Field>
        <TaskFields orgId={orgId} value={draft} onChange={setDraft} />
      </div>
    </Dialog>
  );
}
