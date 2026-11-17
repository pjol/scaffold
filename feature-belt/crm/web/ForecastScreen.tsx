"use client";

import {
  CRM_FORECAST_LABEL,
  crmMoney,
  crmMoneyInput,
  crmParseMoney,
  errorMessage,
  useAccessTeams,
  useCrmChangeGoal,
  useCrmForecast,
  useCrmGoals,
  useCrmPipelines,
  type CrmForecastCell,
  type CrmGoal,
  type CrmGoalMetric,
  type CrmGoalPeriod,
  type CrmUserRef,
} from "@scaffoldapp/shared";
import { Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, InfoTip, Input, Menu, MenuItem, MenuSeparator, SegmentedControl, Select, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Goal, MoreHorizontal, Pencil, Plus, Target, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { CrmFrame, useCrm } from "./CrmFrame";
import { MemberPicker, PersonChip, pipelinesFor } from "./parts";

/** A goal's period in words: "November 2026", "Oct – Dec 2026", "Nov 2026 – Jan 2027" (end is exclusive). */
function periodText(start: string, end: string): string {
  const s = new Date(`${start}T00:00:00`);
  const e = new Date(`${end}T00:00:00`);
  e.setDate(e.getDate() - 1);
  if (Number.isNaN(s.getTime()) || Number.isNaN(e.getTime())) return start;
  if (s.getMonth() === e.getMonth() && s.getFullYear() === e.getFullYear()) return s.toLocaleDateString(undefined, { month: "long", year: "numeric" });
  const short = (d: Date, year: boolean) => d.toLocaleDateString(undefined, year ? { month: "short", year: "numeric" } : { month: "short" });
  return `${short(s, s.getFullYear() !== e.getFullYear())} – ${short(e, true)}`;
}

/** /crm/forecast: deals by close month and owner, by forecast category, and sales goals. */
export function CrmForecastScreen() {
  return (
    <CrmFrame>
      <div className="flex flex-col gap-section">
        <Link href="/crm/reports" className="inline-flex w-fit items-center gap-1 type-label text-fg-muted hover:text-fg hover:underline">
          <ArrowLeft className="size-4" aria-hidden />
          Reports
        </Link>
        <Forecast />
        <Goals />
      </div>
    </CrmFrame>
  );
}

function thisMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function monthLabel(m: string): string {
  const [y, mo] = m.split("-").map(Number);
  return new Date(y ?? 1970, (mo ?? 1) - 1, 1).toLocaleDateString(undefined, { month: "short", year: "numeric" });
}

const CATS = ["closed", "commit", "best_case", "pipeline"] as const;

function Forecast() {
  const { orgId, settings } = useCrm();
  const pipelines = pipelinesFor(useCrmPipelines(orgId).data, "deal");
  const [from, setFrom] = useState(thisMonth());
  const [months, setMonths] = useState("3");
  const [pipelineId, setPipelineId] = useState("all");
  const [currency, setCurrency] = useState(settings.currency);
  const f = useCrmForecast(orgId, { from, months: Number(months), ...(pipelineId !== "all" ? { pipeline_id: pipelineId } : {}), currency });
  const money = (n: number) => crmMoney(n, f.data?.currency ?? currency, { short: true });
  const cell = (c: CrmForecastCell, strong = false) => (
    <div className="flex flex-col items-end gap-0.5 tabular-nums">
      <span className={strong ? "font-semibold text-fg" : "text-fg"}>{money(c.closed + c.commit)}</span>
      <span className="type-caption text-fg-muted">
        +{money(c.best_case)} best · {money(c.pipeline)} pipeline
      </span>
      <span className="type-caption text-fg-subtle">{money(c.weighted)} weighted</span>
    </div>
  );
  return (
    <section className="flex flex-col gap-4" aria-label="Forecast">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="flex flex-1 items-center gap-2 type-title-sm text-fg">
          Forecast
          <InfoTip
            label="About the forecast"
            content="Deals you can see, by close month and owner. The headline is closed won plus commit; best case and pipeline follow. A deal's category is the one set on it, else what its stage's probability implies (80 %+ commit, 50 %+ best case). Weighted is each open deal's amount × its stage probability."
          />
        </h2>
        <Input type="month" aria-label="First month" value={from} onChange={(e) => e.target.value && setFrom(e.target.value)} wrapperClassName="w-48" />
        <SegmentedControl
          aria-label="Months"
          fullWidth={false}
          value={months}
          onValueChange={setMonths}
          options={[
            { value: "3", label: "3 months" },
            { value: "6", label: "6" },
            { value: "12", label: "12" },
          ]}
        />
        {pipelines.length > 1 ? (
          <Select aria-label="Pipeline" className="w-48" value={pipelineId} onValueChange={setPipelineId} options={[{ value: "all", label: "All pipelines" }, ...pipelines.map((p) => ({ value: p.id, label: p.name }))]} />
        ) : null}
        <Input aria-label="Currency" value={currency} maxLength={3} onChange={(e) => /^[A-Za-z]{0,3}$/.test(e.target.value) && setCurrency(e.target.value.toUpperCase())} onBlur={() => currency.length !== 3 && setCurrency(settings.currency)} wrapperClassName="w-20" />
      </div>
      {f.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : f.isError ? (
        <ErrorState title="Couldn't load the forecast" info={f.error.message} onRetry={() => void f.refetch()} retrying={f.isRefetching} />
      ) : f.data.rows.length === 0 ? (
        <Card padding="compact">
          <EmptyState icon={<Target />} title="No deals close in these months" info={`Only deals in ${f.data.currency} with a close date in the range count.`} />
        </Card>
      ) : (
        <Card padding="none" className="overflow-x-auto">
          <table className="w-full min-w-max type-label">
            <caption className="sr-only">Forecast by owner and close month in {f.data.currency}</caption>
            <thead>
              <tr className="border-b border-border text-left">
                <th scope="col" className="sticky left-0 bg-surface px-4 py-3 font-semibold text-fg-muted">
                  Owner
                </th>
                {f.data.months.map((m) => (
                  <th key={m} scope="col" className="px-4 py-3 text-right font-semibold text-fg-muted">
                    {monthLabel(m)}
                  </th>
                ))}
                <th scope="col" className="px-4 py-3 text-right font-semibold text-fg-muted">
                  Total
                </th>
              </tr>
            </thead>
            <tbody>
              {f.data.rows.map((r) => (
                <tr key={r.owner?.id ?? "none"} className="border-b border-border align-top">
                  <th scope="row" className="sticky left-0 bg-surface px-4 py-3 text-left font-normal">
                    {r.owner ? <PersonChip user={r.owner} size="sm" /> : <span className="text-fg-muted">No owner</span>}
                  </th>
                  {r.cells.map((c) => (
                    <td key={c.month} className="px-4 py-3">
                      {c.count ? cell(c) : <span className="block text-right text-fg-subtle">—</span>}
                    </td>
                  ))}
                  <td className="px-4 py-3">{cell(r.total, true)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <th scope="row" className="sticky left-0 bg-surface px-4 py-3 text-left font-semibold text-fg">
                  Everyone
                </th>
                {f.data.totals.map((c) => (
                  <td key={c.month} className="px-4 py-3">
                    {cell(c, true)}
                  </td>
                ))}
                <td className="px-4 py-3">{cell(f.data.total, true)}</td>
              </tr>
            </tfoot>
          </table>
        </Card>
      )}
      <p className="type-caption text-fg-muted">
        {CATS.map((c) => CRM_FORECAST_LABEL[c]).join(" · ")} — {CRM_FORECAST_LABEL.omitted} deals (lost, or set to omitted) don&apos;t count.
      </p>
    </section>
  );
}

const METRIC_LABEL: Record<CrmGoalMetric, string> = { revenue_won: "Revenue won", deals_won: "Deals won", deals_created: "Deals created" };
const PERIOD_LABEL: Record<CrmGoalPeriod, string> = { month: "Month", quarter: "Quarter", year: "Year" };

function Goals() {
  const { orgId, settings } = useCrm();
  const goals = useCrmGoals(orgId);
  const change = useCrmChangeGoal(orgId);
  const can = settings.can.manage_settings;
  const [editing, setEditing] = useState<CrmGoal | "new" | null>(null);
  const [deleting, setDeleting] = useState<CrmGoal | null>(null);
  return (
    <section className="flex flex-col gap-4" aria-label="Goals">
      <div className="flex items-center gap-3">
        <h2 className="flex flex-1 items-center gap-2 type-title-sm text-fg">
          Goals this period
          <InfoTip label="About goals" content="Progress counts the deals you can see, so it can read lower for you than for the goal's owner." />
        </h2>
        {can ? (
          <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New goal
          </Button>
        ) : null}
      </div>
      {goals.isPending ? (
        <SkeletonGroup label="Loading goals" className="grid gap-4 md:grid-cols-2">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-24 w-full" />
        </SkeletonGroup>
      ) : goals.isError ? (
        <ErrorState compact title="Couldn't load goals" info={goals.error.message} onRetry={() => void goals.refetch()} />
      ) : goals.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState compact icon={<Goal />} title="No goals for this period" />
        </Card>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {goals.data.items.map((g) => {
            const pct = Math.min(100, Math.round((g.progress / g.target) * 100));
            const fmt = (n: number) => (g.metric === "revenue_won" ? crmMoney(n, g.currency ?? settings.currency, { short: true }) : n.toLocaleString());
            return (
              <li key={g.id}>
                <Card padding="compact" className="flex flex-col gap-3">
                  <div className="flex items-start gap-2">
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span className="type-label font-semibold text-fg">{METRIC_LABEL[g.metric]}</span>
                      <span className="flex items-center gap-2 type-caption text-fg-muted">
                        {g.user ? <PersonChip user={g.user} /> : g.team ? g.team.name : ""} · {periodText(g.period_start, g.period_end)}
                      </span>
                    </div>
                    {can ? (
                      <Menu trigger={<IconButton label="Goal actions" size="sm" icon={<MoreHorizontal />} />}>
                        <MenuItem icon={<Pencil />} onSelect={() => setEditing(g)}>
                          Change target
                        </MenuItem>
                        <MenuSeparator />
                        <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(g)}>
                          Delete
                        </MenuItem>
                      </Menu>
                    ) : null}
                  </div>
                  <div className="flex items-baseline justify-between gap-2 type-label tabular-nums">
                    <span className="font-semibold text-fg">{fmt(g.progress)}</span>
                    <span className="text-fg-muted">of {fmt(g.target)}</span>
                  </div>
                  <div className="h-2 overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label={`${METRIC_LABEL[g.metric]}: ${pct} %`}>
                    <div className="h-full rounded-pill bg-primary transition-[width] duration-(--app-duration-base) motion-reduce:transition-none" style={{ width: `${pct}%` }} />
                  </div>
                </Card>
              </li>
            );
          })}
        </ul>
      )}
      {editing ? <GoalDialog goal={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title="Delete this goal?"
        confirmLabel="Delete goal"
        onConfirm={() => (deleting ? change.mutateAsync({ op: "delete", id: deleting.id }).then(() => toast.success("Goal deleted"), (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it.")))) : undefined)}
      >
        Deals aren&apos;t affected.
      </ConfirmDialog>
    </section>
  );
}

function GoalDialog({ goal, onClose }: { goal: CrmGoal | null; onClose: () => void }) {
  const { orgId, settings } = useCrm();
  const change = useCrmChangeGoal(orgId);
  const teams = useAccessTeams(orgId).data?.items ?? [];
  const [metric, setMetric] = useState<CrmGoalMetric>(goal?.metric ?? "revenue_won");
  const [period, setPeriod] = useState<CrmGoalPeriod>(goal?.period ?? "quarter");
  const [start, setStart] = useState(goal?.period_start ?? new Date().toISOString().slice(0, 10));
  const [who, setWho] = useState<"person" | "team">(goal?.team ? "team" : "person");
  const [person, setPerson] = useState<CrmUserRef | null>(goal?.user ?? null);
  const [teamId, setTeamId] = useState(goal?.team?.id ?? "");
  const currency = goal?.currency ?? settings.currency;
  const [target, setTarget] = useState(goal ? (goal.metric === "revenue_won" ? crmMoneyInput(goal.target, currency) : String(goal.target)) : "");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    const t = metric === "revenue_won" ? crmParseMoney(target, currency) : Number(target);
    if (t === null || !Number.isFinite(t) || t <= 0) return setError("Enter a target above 0.");
    if (goal) {
      change.mutate({ op: "update", id: goal.id, target: t }, { onSuccess: onClose, onError: (e) => setError(errorMessage(e, "Couldn't save the goal.")) });
      return;
    }
    if (who === "person" && !person) return setError("Choose a person.");
    if (who === "team" && !teamId) return setError("Choose a team.");
    change.mutate(
      { op: "create", body: { metric, period, period_start: start, target: t, ...(who === "person" && person ? { user_id: person.id } : { team_id: teamId }), ...(metric === "revenue_won" ? { currency } : {}) } },
      { onSuccess: onClose, onError: (e) => setError(errorMessage(e, "Couldn't save the goal.")) },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={goal ? "Change target" : "New goal"}
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={change.isPending} onClick={submit}>
            Save goal
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {!goal ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Goal">
                <Select value={metric} onValueChange={(v) => setMetric(v as CrmGoalMetric)} options={(Object.keys(METRIC_LABEL) as CrmGoalMetric[]).map((k) => ({ value: k, label: METRIC_LABEL[k] }))} />
              </Field>
              <Field label="Period" info="Starts on the first day of the month, quarter or year that holds the date.">
                <div className="flex gap-2">
                  <Select className="w-32" value={period} onValueChange={(v) => setPeriod(v as CrmGoalPeriod)} options={(Object.keys(PERIOD_LABEL) as CrmGoalPeriod[]).map((k) => ({ value: k, label: PERIOD_LABEL[k] }))} />
                  <Input type="date" aria-label="Period" value={start} onChange={(e) => setStart(e.target.value)} wrapperClassName="flex-1" />
                </div>
              </Field>
            </div>
            <Field label="For">
              <SegmentedControl
                value={who}
                onValueChange={(v) => setWho(v)}
                options={[
                  { value: "person", label: "A person" },
                  { value: "team", label: "A team" },
                ]}
              />
            </Field>
            {who === "person" ? (
              person ? (
                <div className="flex items-center gap-2">
                  <PersonChip user={person} size="sm" />
                  <Button variant="ghost" size="sm" onClick={() => setPerson(null)}>
                    Change
                  </Button>
                </div>
              ) : (
                <MemberPicker orgId={orgId} label="Find a person" onPick={setPerson} />
              )
            ) : (
              <Select aria-label="Team" value={teamId || undefined} placeholder="Choose a team" onValueChange={setTeamId} options={teams.map((t) => ({ value: t.id, label: t.name }))} />
            )}
          </>
        ) : null}
        <Field label="Target" error={error}>
          <Input value={target} inputMode="decimal" onChange={(e) => setTarget(e.target.value)} {...(metric === "revenue_won" ? { trailing: <span className="px-2 type-caption">{currency}</span> } : {})} />
        </Field>
      </div>
    </Dialog>
  );
}
