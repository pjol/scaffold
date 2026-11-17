"use client";

import {
  errorMessage,
  useDeleteReposLabel,
  useDeleteReposMilestone,
  useReposLabels,
  useReposMilestones,
  useSaveReposLabel,
  useSaveReposMilestone,
  type ReposLabelColor,
  type ReposLabelRow,
  type ReposMilestone,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, Skeleton, SkeletonGroup, Tab, Tabs, TabsList, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Milestone as MilestoneIcon, MoreHorizontal, Pencil, Plus, Tag, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { LabelBadge } from "./issueParts";
import { Body } from "./issueParts";
import { repoHref, useRepoContext } from "./RepoLayout";

const COLORS: { value: ReposLabelColor; label: string; swatch: string }[] = [
  { value: "neutral", label: "Neutral", swatch: "bg-fg-subtle" },
  { value: "primary", label: "Primary", swatch: "bg-primary" },
  { value: "accent", label: "Accent", swatch: "bg-accent" },
  { value: "success", label: "Success", swatch: "bg-success" },
  { value: "warning", label: "Warning", swatch: "bg-warning" },
  { value: "danger", label: "Danger", swatch: "bg-danger" },
  { value: "info", label: "Info", swatch: "bg-info" },
];

/** Labels: what's in use on open issues and pull requests; people who can edit add, rename, recolour and delete. */
export function LabelsScreen() {
  const repo = useRepoContext();
  const labels = useReposLabels(repo.id);
  const [editing, setEditing] = useState<ReposLabelRow | "new" | null>(null);
  const [deleting, setDeleting] = useState<ReposLabelRow | null>(null);
  const del = useDeleteReposLabel(repo.id);
  const rows = labels.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="flex-1 type-title text-fg">
          {rows.length} {rows.length === 1 ? "label" : "labels"}
        </h2>
        <Button asChild variant="ghost" size="sm">
          <Link href={repoHref(repo.id, "issues")}>Issues</Link>
        </Button>
        {repo.can_push ? (
          <Button size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New label
          </Button>
        ) : null}
      </div>
      <Card padding="none">
        {labels.isPending ? (
          <SkeletonGroup label="Loading labels" className="flex flex-col divide-y divide-border">
            {Array.from({ length: 4 }, (_, i) => (
              <div key={i} className="flex min-h-tap-lg items-center gap-3 px-4">
                <Skeleton className="h-5 w-24" />
                <Skeleton className="h-4" width={`${30 + i * 10}%`} />
              </div>
            ))}
          </SkeletonGroup>
        ) : labels.isError ? (
          <ErrorState info={errorMessage(labels.error, "Try again.")} onRetry={() => void labels.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Tag />} title="No labels" action={repo.can_push ? <Button variant="outline" onClick={() => setEditing("new")}>New label</Button> : undefined} />
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((l) => (
              <li key={l.id} className="flex min-h-tap-lg flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2">
                <Link href={repoHref(repo.id, "issues", { q: `is:open label:${/\s/.test(l.name) ? `"${l.name}"` : l.name}` })} className="w-44 shrink-0 focus-visible:outline-2 focus-visible:outline-ring">
                  <LabelBadge label={l} size="md" />
                </Link>
                <span className="min-w-0 flex-1 type-label text-fg-muted">{l.description}</span>
                <span className="type-caption tabular-nums text-fg-muted">
                  {l.open_issues} open {l.open_issues === 1 ? "issue" : "issues"} · {l.open_pulls} open {l.open_pulls === 1 ? "pull request" : "pull requests"}
                </span>
                {repo.can_push ? (
                  <Menu trigger={<IconButton label={`Actions for ${l.name}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
                    <MenuItem icon={<Pencil />} onSelect={() => setEditing(l)}>
                      Edit
                    </MenuItem>
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(l)}>
                      Delete
                    </MenuItem>
                  </Menu>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {editing ? <LabelDialog repoId={repo.id} label={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete the ${deleting?.name ?? ""} label?`}
        tone="danger"
        confirmLabel="Delete label"
        onConfirm={() => del.mutateAsync(deleting!.id)}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        It comes off every issue and pull request that has it.
      </ConfirmDialog>
    </div>
  );
}

function LabelDialog({ repoId, label, onClose }: { repoId: string; label: ReposLabelRow | null; onClose: () => void }) {
  const save = useSaveReposLabel(repoId);
  const [name, setName] = useState(label?.name ?? "");
  const [color, setColor] = useState<ReposLabelColor>(label?.color ?? "primary");
  const [description, setDescription] = useState(label?.description ?? "");
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={label ? `Edit ${label.name}` : "New label"}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            loading={save.isPending}
            disabled={!name.trim()}
            onClick={async () => {
              try {
                await save.mutateAsync({ ...(label ? { labelId: label.id } : {}), body: { name, color, description } });
                onClose();
              } catch (e) {
                toast.error(errorMessage(e, "Couldn't save the label."));
              }
            }}
          >
            {label ? "Save" : "Create label"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex min-h-8 items-center">
          <LabelBadge label={{ name: name || "Label preview", color, description: "" }} size="md" />
        </div>
        <Field label="Name">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={50} autoFocus />
        </Field>
        <Field label="Description">
          <Input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={100} />
        </Field>
        <fieldset className="flex flex-col gap-2">
          <legend className="type-label font-semibold text-fg">Colour</legend>
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Colour">
            {COLORS.map((c) => (
              <button
                key={c.value}
                type="button"
                role="radio"
                aria-checked={color === c.value}
                aria-label={c.label}
                onClick={() => setColor(c.value)}
                className={cn(
                  "flex size-9 items-center justify-center rounded-pill border-2 focus-visible:outline-2 focus-visible:outline-ring",
                  color === c.value ? "border-fg" : "border-transparent",
                )}
              >
                <span className={cn("size-6 rounded-pill", c.swatch)} aria-hidden />
              </button>
            ))}
          </div>
        </fieldset>
      </div>
    </Dialog>
  );
}

// ---- milestones ------------------------------------------------------------------

/** Milestones with progress (closed over all items), due dates, open/closed. */
export function MilestonesScreen() {
  const repo = useRepoContext();
  const [state, setState] = useUrlState<"open" | "closed">("state", "open");
  const ms = useReposMilestones(repo.id, state);
  const [editing, setEditing] = useState<ReposMilestone | "new" | null>(null);
  const [deleting, setDeleting] = useState<ReposMilestone | null>(null);
  const save = useSaveReposMilestone(repo.id);
  const del = useDeleteReposMilestone(repo.id);
  const rows = ms.data?.items ?? [];
  const today = new Date().toISOString().slice(0, 10);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <Tabs value={state} onValueChange={(v) => setState(v as "open" | "closed")} variant="pill">
          <TabsList aria-label="State">
            <Tab value="open">Open</Tab>
            <Tab value="closed">Closed</Tab>
          </TabsList>
        </Tabs>
        <span className="flex-1" />
        <Button asChild variant="ghost" size="sm">
          <Link href={repoHref(repo.id, "issues")}>Issues</Link>
        </Button>
        {repo.can_push ? (
          <Button size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New milestone
          </Button>
        ) : null}
      </div>
      <Card padding="none">
        {ms.isPending ? (
          <SkeletonGroup label="Loading milestones" className="flex flex-col divide-y divide-border">
            {Array.from({ length: 3 }, (_, i) => (
              <div key={i} className="flex flex-col gap-2 px-4 py-4">
                <Skeleton className="h-5 w-48" />
                <Skeleton className="h-2 w-full" />
              </div>
            ))}
          </SkeletonGroup>
        ) : ms.isError ? (
          <ErrorState info={errorMessage(ms.error, "Try again.")} onRetry={() => void ms.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<MilestoneIcon />}
            title={state === "open" ? "No open milestones" : "No closed milestones"}
            action={repo.can_push && state === "open" ? <Button variant="outline" onClick={() => setEditing("new")}>New milestone</Button> : undefined}
          />
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((m) => {
              const total = m.open_items + m.closed_items;
              const pct = total ? Math.round((m.closed_items / total) * 100) : 0;
              const overdue = m.state === "open" && !!m.due_on && m.due_on < today;
              return (
                <li key={m.id} className="flex flex-col gap-3 px-4 py-4 md:flex-row md:items-start">
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <Link href={repoHref(repo.id, "issues", { q: `milestone:"${m.title}"` })} className="type-title-sm text-fg hover:underline">
                      {m.title}
                    </Link>
                    <span className="flex flex-wrap items-center gap-2 type-caption text-fg-muted">
                      {m.due_on ? overdue ? <Badge tone="danger">Past due {m.due_on}</Badge> : <span>Due {m.due_on}</span> : <span>No due date</span>}
                      {m.state === "closed" && m.closed_at ? <span>· closed {m.closed_at.slice(0, 10)}</span> : null}
                    </span>
                    {m.description ? <Body source={m.description} repoId={repo.id} /> : null}
                  </div>
                  <div className="flex w-full flex-col gap-2 md:w-72">
                    <div className="h-2 overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`${m.title}: ${pct}% done`}>
                      <div className="h-full rounded-pill bg-success" style={{ width: `${pct}%` }} />
                    </div>
                    <span className="flex flex-wrap gap-x-3 type-caption text-fg-muted">
                      <span className="font-semibold text-fg">{pct}% done</span>
                      <span>{m.open_items} open</span>
                      <span>{m.closed_items} closed</span>
                    </span>
                    {repo.can_push ? (
                      <div className="flex gap-1">
                        <Button size="sm" variant="ghost" onClick={() => setEditing(m)}>
                          Edit
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          loading={save.isPending && save.variables?.milestoneId === m.id}
                          onClick={() => save.mutate({ milestoneId: m.id, body: { state: m.state === "open" ? "closed" : "open" } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
                        >
                          {m.state === "open" ? "Close" : "Reopen"}
                        </Button>
                        <Button size="sm" variant="ghost" className="text-danger-strong" onClick={() => setDeleting(m)}>
                          Delete
                        </Button>
                      </div>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </Card>
      {editing ? <MilestoneDialog repoId={repo.id} milestone={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete the ${deleting?.title ?? ""} milestone?`}
        tone="danger"
        confirmLabel="Delete milestone"
        onConfirm={() => del.mutateAsync(deleting!.id)}
        errorText={(e) => errorMessage(e, "Couldn't delete it.")}
      >
        Its issues and pull requests stay, without a milestone.
      </ConfirmDialog>
    </div>
  );
}

function MilestoneDialog({ repoId, milestone, onClose }: { repoId: string; milestone: ReposMilestone | null; onClose: () => void }) {
  const save = useSaveReposMilestone(repoId);
  const [title, setTitle] = useState(milestone?.title ?? "");
  const [due, setDue] = useState(milestone?.due_on ?? "");
  const [description, setDescription] = useState(milestone?.description ?? "");
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={milestone ? `Edit ${milestone.title}` : "New milestone"}
      size="lg"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            loading={save.isPending}
            disabled={!title.trim()}
            onClick={async () => {
              try {
                await save.mutateAsync({ ...(milestone ? { milestoneId: milestone.id } : {}), body: { title, due_on: due, description } });
                onClose();
              } catch (e) {
                toast.error(errorMessage(e, "Couldn't save the milestone."));
              }
            }}
          >
            {milestone ? "Save" : "Create milestone"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={100} autoFocus />
        </Field>
        <Field label="Due date">
          <Input type="date" value={due} onChange={(e) => setDue(e.target.value)} />
        </Field>
        <Field label="Description" info="Markdown works here.">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} minRows={3} />
        </Field>
      </div>
    </Dialog>
  );
}
