"use client";

import { crmMainTotal, crmMoney, crmMoneyTotals, useCrmDashboard, useCrmPipelines, useCrmTasks, type CrmDashboard } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, InfoTip, Select, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import { CheckSquare, Handshake } from "lucide-react";
import Link from "next/link";
import { Section } from "@/components/shell/PageHeader";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { PersonChip } from "./parts";
import { TaskRow } from "./TasksScreen";

/** /crm: open pipeline by stage, won and lost this month, activity this week, your tasks. */
export function CrmDashboardScreen() {
  return (
    <CrmFrame>
      <Dashboard />
    </CrmFrame>
  );
}

function Dashboard() {
  const { orgId, settings } = useCrm();
  const pipelines = useCrmPipelines(orgId);
  const [pipelineId, setPipelineId] = useUrlState("pipeline", "");
  const dash = useCrmDashboard(orgId, pipelineId || undefined);
  const main = settings.currency;

  if (dash.isError) return <ErrorState title="Couldn't load the dashboard" info={dash.error.message} onRetry={() => void dash.refetch()} retrying={dash.isRefetching} />;
  const d = dash.data;
  return (
    <div className="flex flex-col gap-section">
      {pipelines.data && pipelines.data.length > 1 ? (
        <Select
          aria-label="Pipeline"
          className="w-full sm:w-64"
          value={pipelineId || pipelines.data[0]?.id}
          onValueChange={(v) => setPipelineId(v === pipelines.data?.[0]?.id ? "" : v)}
          options={pipelines.data.map((p) => ({ value: p.id, label: p.name }))}
        />
      ) : null}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Stat label="Open pipeline" info="Every open deal you can see in this pipeline. Weighted multiplies each amount by its stage's probability." loading={!d}>
          {d ? (
            <>
              <span className="type-headline tabular-nums text-fg">{crmMoneyTotals(d.open_totals, main, "amount", { short: true })}</span>
              <span className="type-caption text-fg-muted">{crmMoneyTotals(d.open_totals, main, "weighted", { short: true })} weighted</span>
            </>
          ) : null}
        </Stat>
        <Stat label="Won this month" loading={!d}>
          {d ? (
            <>
              <span className="type-headline tabular-nums text-success-strong">{crmMoneyTotals(d.won_this_month.totals, main, "amount", { short: true })}</span>
              <span className="type-caption text-fg-muted">{plural(d.won_this_month.count, "deal")}</span>
            </>
          ) : null}
        </Stat>
        <Stat label="Lost this month" loading={!d}>
          {d ? (
            <>
              <span className="type-headline tabular-nums text-fg">{crmMoneyTotals(d.lost_this_month.totals, main, "amount", { short: true })}</span>
              <span className="type-caption text-fg-muted">{plural(d.lost_this_month.count, "deal")}</span>
            </>
          ) : null}
        </Stat>
        <Stat label="Your tasks" loading={!d}>
          {d ? (
            <>
              <span className={cn("type-headline tabular-nums", d.my_tasks.overdue > 0 ? "text-danger-strong" : "text-fg")}>{d.my_tasks.overdue + d.my_tasks.today}</span>
              <span className="type-caption text-fg-muted">
                {d.my_tasks.overdue} overdue · {d.my_tasks.today} today
              </span>
            </>
          ) : null}
        </Stat>
      </div>
      <div className="grid gap-section lg:grid-cols-5">
        <Section title="Open pipeline by stage" className="lg:col-span-3" action={<Button asChild variant="ghost" size="sm"><Link href="/crm/deals/board">Board</Link></Button>}>
          <Card padding="compact">{d ? <StageBars d={d} main={main} /> : <BarsSkeleton />}</Card>
        </Section>
        <Section title="Due soon" className="lg:col-span-2" action={<Button asChild variant="ghost" size="sm"><Link href="/crm/tasks">All tasks</Link></Button>}>
          <DueSoon />
        </Section>
      </div>
      <Section title="Activity this week" info="Notes, calls, emails and meetings logged since Monday, and tasks completed, on records you can see.">
        <Card padding="none">
          {!d ? (
            <BarsSkeleton />
          ) : d.activity_this_week.length === 0 ? (
            <EmptyState compact icon={<Handshake />} title="Nothing logged this week" />
          ) : (
            <Table label="Activity this week" className="min-w-160">
              <THead>
                <TH>Person</TH>
                <TH align="right">Notes</TH>
                <TH align="right">Calls</TH>
                <TH align="right">Emails</TH>
                <TH align="right">Meetings</TH>
                <TH align="right">Tasks done</TH>
                <TH align="right">Total</TH>
              </THead>
              <tbody>
                {d.activity_this_week.map((a) => (
                  <TR key={a.user.id}>
                    <TD>
                      <PersonChip user={a.user} />
                    </TD>
                    <TD align="right">{a.notes}</TD>
                    <TD align="right">{a.calls}</TD>
                    <TD align="right">{a.emails}</TD>
                    <TD align="right">{a.meetings}</TD>
                    <TD align="right">{a.tasks_done}</TD>
                    <TD align="right" className="font-semibold">
                      {a.total}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </Section>
    </div>
  );
}

function plural(n: number, one: string) {
  return `${n} ${one}${n === 1 ? "" : "s"}`;
}

function Stat({ label, info, loading, children }: { label: string; info?: string; loading: boolean; children: React.ReactNode }) {
  return (
    <Card padding="compact" className="flex min-w-0 flex-col gap-1">
      <span className="flex items-center gap-1 type-label text-fg-muted">
        {label}
        {info ? <InfoTip content={info} /> : null}
      </span>
      {loading ? (
        <SkeletonGroup label={`Loading ${label}`} className="flex flex-col gap-2">
          <Skeleton className="h-9 w-28" />
          <Skeleton shape="text" className="h-3 w-20" />
        </SkeletonGroup>
      ) : (
        <div className="flex min-w-0 flex-col gap-1 truncate">{children}</div>
      )}
    </Card>
  );
}

function StageBars({ d, main }: { d: CrmDashboard; main: string }) {
  const max = Math.max(1, ...d.open_by_stage.map((s) => crmMainTotal(s.totals, main)));
  if (d.open_by_stage.every((s) => s.count === 0)) {
    return (
      <EmptyState
        compact
        icon={<Handshake />}
        title="No open deals"
        action={
          <Button asChild size="auto">
            <Link href="/crm/deals">Deals</Link>
          </Button>
        }
      />
    );
  }
  return (
    <ul className="flex flex-col gap-3" aria-label="Open deals by stage">
      {d.open_by_stage.map((s) => {
        const amount = crmMainTotal(s.totals, main);
        return (
          <li key={s.stage_id} className="flex flex-col gap-1">
            <div className="flex items-baseline gap-2 type-label">
              <span className="min-w-0 flex-1 truncate font-semibold text-fg">{s.name}</span>
              <span className="text-fg-muted tabular-nums">{s.count}</span>
              <span className="w-24 text-right tabular-nums text-fg">{crmMoneyTotals(s.totals, main, "amount", { short: true })}</span>
            </div>
            <div className="h-2 w-full overflow-hidden rounded-pill bg-sunken" aria-hidden>
              <div className="h-full rounded-pill bg-primary transition-[width] duration-(--app-duration-base)" style={{ width: `${Math.max(amount > 0 ? 2 : 0, (amount / max) * 100)}%` }} />
            </div>
          </li>
        );
      })}
      <li className="flex items-baseline gap-2 border-t border-border pt-3 type-label">
        <span className="flex-1 font-semibold text-fg">Weighted forecast</span>
        <span className="tabular-nums font-semibold text-fg">{crmMoney(crmMainTotal(d.open_totals, main, "weighted"), main)}</span>
      </li>
    </ul>
  );
}

function BarsSkeleton() {
  return (
    <SkeletonGroup label="Loading" className="flex flex-col gap-4 p-4">
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="flex flex-col gap-2">
          <Skeleton shape="text" className="h-4 w-1/2" />
          <Skeleton className="h-2 w-full" />
        </div>
      ))}
    </SkeletonGroup>
  );
}

function DueSoon() {
  const { orgId } = useCrm();
  const overdue = useCrmTasks(orgId, "overdue");
  const today = useCrmTasks(orgId, "today");
  const items = [...(overdue.data?.items ?? []), ...(today.data?.items ?? [])].slice(0, 6);
  if (overdue.isPending || today.isPending)
    return (
      <Card padding="none">
        <BarsSkeleton />
      </Card>
    );
  if (overdue.isError) return <ErrorState compact title="Couldn't load tasks" onRetry={() => void overdue.refetch()} />;
  return (
    <Card padding="none" className="p-1">
      {items.length === 0 ? (
        <EmptyState compact icon={<CheckSquare />} title="Nothing due today" />
      ) : (
        <ul className="flex flex-col">
          {items.map((t) => (
            <TaskRow key={t.id} task={t} compact />
          ))}
        </ul>
      )}
    </Card>
  );
}
