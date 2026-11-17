"use client";

import { errorMessage, useCaseAction, useMe, useModerationCase, type CaseAction, type CaseDetail, type CaseUser, type ModerationDecision } from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, Dialog, EmptyState, ErrorState, Field, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { Ban, Check, ChevronLeft, CircleSlash, EyeOff, FileQuestion, MessageSquareWarning, PauseCircle, SquareArrowOutUpRight } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { StatusBadge } from "@/components/admin/badges";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { ACTION_LABEL, categoryLabel, DECISION_LABEL, DECISION_TONE, formatAge, kindLabel, QUESTION_LABEL, reasonLabel, ruleLabel } from "./labels";

const ACTIONS: Record<CaseAction, { label: string; title: string; confirm: string; tone: "primary" | "danger"; icon: ReactNode }> = {
  dismiss: { label: "Dismiss", title: "Dismiss this case?", confirm: "Dismiss", tone: "primary", icon: <CircleSlash /> },
  remove: { label: "Remove content", title: "Remove this content?", confirm: "Remove", tone: "danger", icon: <EyeOff /> },
  warn: { label: "Warn author", title: "Warn the author?", confirm: "Send warning", tone: "primary", icon: <MessageSquareWarning /> },
  suspend: { label: "Suspend author", title: "Suspend the author?", confirm: "Suspend", tone: "danger", icon: <PauseCircle /> },
  ban: { label: "Ban author", title: "Ban the author?", confirm: "Ban", tone: "danger", icon: <Ban /> },
  resolve: { label: "Resolve", title: "Resolve this case?", confirm: "Resolve", tone: "primary", icon: <Check /> },
};

const ACTION_BODY: Record<CaseAction, string> = {
  dismiss: "No rule was broken. Content hidden by reports comes back where the feature supports it.",
  remove: "The content is taken down for everyone. The author is told something they posted was removed.",
  warn: "The author gets a warning notification. Repeat reports can lead to suspension.",
  suspend: "The author can still sign in and read, but can't post or change anything until reinstated.",
  ban: "The author can't sign in until reinstated.",
  resolve: "Close the case without another action, for example when it was handled elsewhere.",
};

function personName(u: CaseUser | null, fallback: string): string {
  if (!u) return fallback;
  return u.display_name || (u.status === "deleted" ? "Deleted user" : u.email);
}

function BackToQueue() {
  return (
    <Link href="/admin/moderation" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      Moderation
    </Link>
  );
}

function Media({ urls }: { urls: string[] }) {
  if (urls.length === 0) return null;
  return (
    <ul className="flex flex-wrap gap-2">
      {urls.map((u, i) => (
        <li key={u}>
          <a href={u} target="_blank" rel="noreferrer" className="block overflow-hidden rounded-md border border-border">
            {/* Evidence comes from presigned or public storage URLs of any host, so next/image can't be configured for it. */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img src={u} alt={`Attachment ${i + 1}`} className="size-24 object-cover" loading="lazy" />
          </a>
        </li>
      ))}
    </ul>
  );
}

/** Screening holds its content until staff approve (dismiss) or remove it. */
function isHeld(c: CaseDetail): boolean {
  return c.source === "auto" && c.status === "open" && c.auto_hidden;
}

function ActionDialog({ c, action, onClose }: { c: CaseDetail; action: CaseAction | null; onClose: () => void }) {
  const act = useCaseAction();
  const [note, setNote] = useState("");
  const [removeContent, setRemoveContent] = useState(false);
  const [notify, setNotify] = useState(true);
  const content = c.target_kind !== "user";
  const held = isHeld(c);
  const a = action ? (held && action === "dismiss" ? { ...ACTIONS.dismiss, title: "Approve this content?", confirm: "Approve" } : ACTIONS[action]) : null;
  const authorAction = action === "warn" || action === "suspend" || action === "ban";
  const offerRemove = content && (!c.hidden || held) && authorAction;
  const mustRemove = held && authorAction;

  const submit = async () => {
    if (!action) return;
    try {
      await act.mutateAsync({ id: c.id, action, note: note.trim(), remove_content: offerRemove && (removeContent || mustRemove), notify_reporters: notify });
      toast.success(`Case ${ACTION_LABEL[action].toLowerCase()}`);
      setNote("");
      setRemoveContent(false);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't apply that. Try again."));
    }
  };

  return (
    <Dialog
      open={action !== null}
      onOpenChange={(o) => !o && !act.isPending && onClose()}
      title={a?.title ?? ""}
      size="md"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={act.isPending} onClick={onClose}>
            Cancel
          </Button>
          <Button variant={a?.tone ?? "primary"} size="auto" loading={act.isPending} onClick={() => void submit()}>
            {a?.confirm}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-6">
        {action && <p className="text-fg-muted">{held && action === "dismiss" ? "The content becomes visible to everyone and the author is told." : ACTION_BODY[action]}</p>}
        <Field label="Note" info="Staff only. Saved with the case and the audit log.">
          <TextArea value={note} onChange={(e) => setNote(e.target.value.slice(0, 2000))} minRows={2} maxRows={6} autoGrow />
        </Field>
        <div className="flex flex-col gap-3">
          {offerRemove && (
            <Checkbox
              checked={removeContent || mustRemove}
              disabled={mustRemove}
              onCheckedChange={setRemoveContent}
              label="Also remove the content"
              {...(mustRemove ? { info: "Held content is approved or removed; an author action on it removes it." } : {})}
            />
          )}
          <Checkbox checked={notify} onCheckedChange={setNotify} label="Tell reporters it was reviewed" info="They hear that it was reviewed, and whether action was taken. Never which action." />
        </div>
      </div>
    </Dialog>
  );
}

function Actions({ c }: { c: CaseDetail }) {
  const me = useMe();
  const [action, setAction] = useState<CaseAction | null>(null);
  const content = c.target_kind !== "user";
  const author = c.author;
  const canActOnAuthor = !!author && author.status !== "deleted" && author.id !== me.data?.id;
  const held = isHeld(c);
  const available: CaseAction[] = [
    "dismiss",
    ...(content && (!c.hidden || held) ? (["remove"] as const) : []),
    ...(canActOnAuthor ? (["warn", "suspend", "ban"] as const) : []),
    ...(held ? [] : (["resolve"] as const)),
  ];
  return (
    <Section title="Action" info="Every action closes the case and is written to the audit log.">
      <div className="flex flex-wrap gap-2">
        {available.map((a) => (
          <Button key={a} variant={ACTIONS[a].tone === "danger" ? "danger-ghost" : "secondary"} size="auto" leftIcon={ACTIONS[a].icon} onClick={() => setAction(a)}>
            {held && a === "dismiss" ? "Approve" : ACTIONS[a].label}
          </Button>
        ))}
      </div>
      <ActionDialog c={c} action={action} onClose={() => setAction(null)} />
    </Section>
  );
}

function percent(p: number): string {
  return `${Math.round(p * 100)}%`;
}

function DecisionBlock({ d }: { d: ModerationDecision }) {
  const answers = d.answers ? Object.entries(d.answers).sort(([a], [b]) => a.localeCompare(b)) : [];
  const model = d.model_status === "ok" ? (d.model ?? "Model") : `Model ${d.model_status.replace(/_/g, " ")}`;
  return (
    <li className="flex flex-col gap-3 py-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={DECISION_TONE[d.action]}>{DECISION_LABEL[d.action]}</Badge>
        {d.edit && <Badge tone="neutral">Edit</Badge>}
        {d.distrusted && <Badge tone="warning">Model answers distrusted</Badge>}
        <span className="type-caption text-fg-muted">{`Rules ${DECISION_LABEL[d.rules_action].toLowerCase()} · ${model}${d.latency_ms !== null ? ` · ${d.latency_ms} ms` : ""}`}</span>
        <span className="ml-auto type-caption text-fg-muted">{formatDateTime(d.created_at)}</span>
      </div>
      {d.hits.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {d.hits.map((h, i) => (
            <li key={`${h.rule}-${i}`} className="flex flex-col gap-1 sm:flex-row sm:items-baseline sm:gap-3">
              <span className="flex shrink-0 items-center gap-2 sm:w-64">
                <Badge tone={DECISION_TONE[h.action]} size="sm">
                  {DECISION_LABEL[h.action]}
                </Badge>
                <span className="type-label font-semibold text-fg">{ruleLabel(h.rule)}</span>
              </span>
              <span className="min-w-0 break-words type-label text-fg-muted">{[h.category ? categoryLabel(h.category) : "", h.detail ?? ""].filter(Boolean).join(" · ")}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="type-label text-fg-muted">No rule fired.</p>
      )}
      {answers.length > 0 && (
        <dl className="grid grid-cols-2 gap-x-6 gap-y-1 sm:grid-cols-3">
          {answers.map(([q, p]) => (
            <div key={q} className="flex items-baseline justify-between gap-2">
              <dt className="truncate type-caption text-fg-muted">{QUESTION_LABEL[q] ?? q}</dt>
              <dd className="type-label tabular-nums text-fg">{percent(p)}</dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  );
}

function Screening({ decisions }: { decisions: ModerationDecision[] }) {
  if (decisions.length === 0) return null;
  return (
    <Section title="Screening" info="Rules run first; the decision model can only raise the action. Newest first.">
      <ul className="flex flex-col divide-y divide-border">
        {decisions.map((d) => (
          <DecisionBlock key={d.id} d={d} />
        ))}
      </ul>
    </Section>
  );
}

/** Admin → Moderation → case: what was reported (as captured), who reported it and why, the author's history, and the staff actions. */
export function ModerationCaseScreen({ id }: { id: string }) {
  const q = useModerationCase(id);

  if (q.isPending) {
    return (
      <SkeletonGroup label="Loading case" className="flex flex-col gap-section">
        <Skeleton className="h-10 w-32" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40" />
        <Skeleton className="h-32" />
      </SkeletonGroup>
    );
  }
  if (q.isError) {
    return (
      <div className="flex flex-col gap-2">
        <BackToQueue />
        <Card padding="compact">
          {q.error.status === 404 ? (
            <EmptyState compact icon={<FileQuestion />} title="Case not found" />
          ) : (
            <ErrorState compact title="Couldn't load this case" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
          )}
        </Card>
      </div>
    );
  }

  const c = q.data;
  const authorName = personName(c.author, "No author");
  return (
    <div className="flex flex-col gap-2">
      <BackToQueue />
      <div className="flex flex-col gap-section">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="type-title text-fg">{kindLabel(c.target_kind)} reported</h1>
          {c.status === "open" ? (
            <Badge tone={c.over_sla ? "danger" : "info"} dot>
              {`Open ${formatAge(c.age_seconds)}`}
            </Badge>
          ) : (
            <Badge tone={c.status === "dismissed" ? "neutral" : "success"}>{c.action ? ACTION_LABEL[c.action] : "Closed"}</Badge>
          )}
          {c.hidden && <Badge tone="warning">{c.auto_hidden ? (c.source === "auto" ? "Held by screening" : "Hidden by reports") : "Removed"}</Badge>}
        </div>

        <Section
          title={c.source === "auto" ? "What was posted" : "What was reported"}
          info={c.source === "auto" ? "The text screening saw, updated when the author edits it." : "Captured when it was first reported, so edits and deletions don't change it."}
        >
          <Card className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-3">
              <AppAvatar name={authorName} src={c.author?.avatar_url ?? null} size="md" />
              {c.author ? (
                <Link href={`/admin/users/${c.author.id}`} className="min-w-0 flex-1 truncate type-body font-semibold text-fg underline-offset-2 hover:underline">
                  {authorName}
                </Link>
              ) : (
                <span className="min-w-0 flex-1 type-body font-semibold text-fg">{authorName}</span>
              )}
              {c.author && <StatusBadge status={c.author.status} />}
            </div>
            {c.target_kind === "user" ? (
              <dl className="flex flex-col gap-1 sm:flex-row sm:items-center sm:gap-4">
                <dt className="type-label text-fg-muted sm:w-40 sm:shrink-0">Name when reported</dt>
                <dd className="min-w-0 break-words type-body text-fg">{c.snapshot.text || "Removed with the account."}</dd>
              </dl>
            ) : c.snapshot.text ? (
              <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{c.snapshot.text}</p>
            ) : (
              <p className="type-body text-fg-muted">{c.author?.status === "deleted" ? "Removed with the author's account." : "No text."}</p>
            )}
            <Media urls={c.media_urls} />
            {c.snapshot.link && c.target_kind !== "user" && !c.hidden && (
              <Button asChild variant="ghost" size="sm" className="self-start" rightIcon={<SquareArrowOutUpRight />}>
                <Link href={c.snapshot.link} target="_blank">
                  Open in app
                </Link>
              </Button>
            )}
          </Card>
        </Section>

        <Screening decisions={c.decisions} />

        {c.reports.length > 0 && (
        <Section title={`Reports (${c.report_count})`}>
          <Card padding="none">
            <ul className="flex flex-col">
              {c.reports.map((r) => {
                const name = personName(r.reporter, "Deleted user");
                const changed = r.snapshot.text !== c.snapshot.text && r.snapshot.text;
                return (
                  <li key={r.id} className="flex flex-col gap-2 border-b border-border p-card last:border-b-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="type-label font-semibold text-fg">{name}</span>
                      <Badge tone="neutral">{reasonLabel(r.reason)}</Badge>
                      <span className="ml-auto type-caption text-fg-muted">{formatDateTime(r.created_at)}</span>
                    </div>
                    {r.note && <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{r.note}</p>}
                    {changed && <p className="max-w-prose whitespace-pre-wrap break-words type-label text-fg-muted">{`Text when this report was sent: ${r.snapshot.text}`}</p>}
                  </li>
                );
              })}
            </ul>
          </Card>
        </Section>
        )}

        {c.author && (
          <Section title="Author history">
            <p className="type-body text-fg">
              {c.author_history.cases === 0
                ? "No other cases."
                : `${c.author_history.cases} other ${c.author_history.cases === 1 ? "case" : "cases"}, ${c.author_history.actioned} with action taken.`}
              {c.author_history.strikes > 0 ? ` ${c.author_history.strikes} active ${c.author_history.strikes === 1 ? "strike" : "strikes"}.` : ""}{" "}
              {c.author_history.cases > 0 && (
                <Link href={`/admin/moderation?status=closed&author_id=${c.author.id}`} className="font-semibold underline underline-offset-2">
                  View
                </Link>
              )}
            </p>
          </Section>
        )}

        {c.status === "open" ? (
          <Actions c={c} />
        ) : (
          <Section title="Outcome">
            <Card className="flex flex-col gap-2">
              <p className="type-body text-fg">{`${c.action ? ACTION_LABEL[c.action] : "Closed"}${c.resolved_at ? ` on ${formatDateTime(c.resolved_at)}` : ""}`}</p>
              {c.resolution_note && <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg-muted">{c.resolution_note}</p>}
            </Card>
          </Section>
        )}
      </div>
    </div>
  );
}
