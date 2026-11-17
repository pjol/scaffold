"use client";

import {
  REPUTATION_STATUS_INFO,
  REPUTATION_STATUS_LABEL,
  formatReputationAmount,
  formatReputationPct,
  formatReputationWeight,
  reputationEquityScale,
  reputationLinePath,
  reputationMetricLabel,
  reputationTierLabel,
  reputationTierTone,
  type Reputation,
  type ReputationComponent,
  type ReputationEquityPoint,
} from "@scaffoldapp/shared";
import { Badge, EmptyState, InfoTip, ListGroup, ListRow, cn, motion } from "@scaffoldapp/ui-web";
import { CheckCircle2, Circle, LineChart } from "lucide-react";
import { Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { ReputationRing } from "./ReputationRing";

function Overview({ rep }: { rep: Reputation }) {
  const rated = rep.status === "rated" && rep.score !== null;
  const info = (
    <span className="flex flex-col gap-1">
      <span>{REPUTATION_STATUS_INFO[rep.status]}</span>
      {rep.computed_at && (
        <span>
          Computed {formatDateTime(rep.computed_at)} with {rep.version}.
        </span>
      )}
    </span>
  );
  return (
    <div className="flex items-center gap-4">
      <ReputationRing score={rep.score} status={rep.status} tier={rep.tier} size={80} />
      <div className="flex min-w-0 flex-col gap-2">
        <p className="flex items-center gap-2 type-title-sm text-fg">
          {rated ? (
            <span className="tabular-nums">
              {rep.score} <span className="type-label text-fg-muted">of 100</span>
            </span>
          ) : (
            <span>{REPUTATION_STATUS_LABEL[rep.status]}</span>
          )}
          <InfoTip content={info} label="About this score" />
        </p>
        {rated && rep.tier && (
          <Badge tone={reputationTierTone(rep.tier)} className="self-start">
            {reputationTierLabel(rep.tier)}
          </Badge>
        )}
        {rep.status === "under_review" && (
          <Badge tone="warning" dot className="self-start">
            Score hidden
          </Badge>
        )}
      </div>
    </div>
  );
}

function Meter({ value, scored }: { value: number; scored: boolean }) {
  return (
    <span className="relative block h-2 flex-1 overflow-hidden rounded-pill bg-sunken" aria-hidden>
      {scored && (
        <motion.span
          className="absolute inset-y-0 left-0 block rounded-pill bg-primary"
          initial={{ width: 0 }}
          animate={{ width: `${Math.max(1, Math.min(100, value))}%` }}
          transition={{ duration: 0.38 }}
        />
      )}
    </span>
  );
}

function ComponentItem({ c }: { c: ReputationComponent }) {
  const metrics = Object.entries(c.metrics);
  return (
    <li className="flex flex-col gap-3 border-b border-border py-4 first:pt-0">
      <div className="flex items-center gap-2">
        <span className={cn("type-label font-semibold", c.scored ? "text-fg" : "text-fg-muted")}>{c.label}</span>
        <InfoTip content={c.detail} label={`How ${c.label.toLowerCase()} is scored`} />
        <span className="ml-auto type-label tabular-nums text-fg">{c.scored ? `${c.points.toFixed(1)} pts` : "Not scored"}</span>
      </div>
      <div className="flex items-center gap-3">
        <Meter value={c.value} scored={c.scored} />
        <span className="w-24 shrink-0 text-right type-caption tabular-nums text-fg-muted">
          {c.scored ? `${c.value.toFixed(1)} × ${formatReputationWeight(c.weight)}` : "0%"}
        </span>
      </div>
      {metrics.length > 0 && (
        <dl className="grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
          {metrics.map(([k, v]) => (
            <div key={k} className="flex items-baseline justify-between gap-3">
              <dt className="type-caption text-fg-muted">{reputationMetricLabel(k)}</dt>
              <dd className="type-caption tabular-nums text-fg">{v}</dd>
            </div>
          ))}
        </dl>
      )}
    </li>
  );
}

/** The time-weighted index (100 = start of the window) as a line over a dashed 100 baseline. */
export function ReputationEquityChart({ points }: { points: ReputationEquityPoint[] }) {
  const W = 600;
  const H = 160;
  if (points.length < 2) {
    return <EmptyState compact icon={<LineChart />} title="Not enough days for a curve yet" />;
  }
  const { xy, min, max, baseline } = reputationEquityScale(points, W, H, 6);
  const last = points[points.length - 1]!;
  return (
    <figure className="flex flex-col gap-2">
      <div className="flex justify-between type-caption tabular-nums text-fg-muted">
        <span>High {max.toFixed(2)}</span>
        <span>Now {Number(last.index).toFixed(2)}</span>
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`Value index over ${points.length} days, from 100 to ${last.index}`} className="h-40 w-full overflow-visible">
        <line x1={0} x2={W} y1={baseline} y2={baseline} className="stroke-border-strong" strokeDasharray="4 4" vectorEffect="non-scaling-stroke" />
        <path d={reputationLinePath(xy)} fill="none" className="stroke-primary" strokeWidth={2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
      </svg>
      <div className="flex justify-between type-caption tabular-nums text-fg-muted">
        <span>Low {min.toFixed(2)}</span>
        <span>{points[0]!.day}</span>
      </div>
    </figure>
  );
}

function Stat({ label, value, info }: { label: string; value: string; info?: string }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="flex items-center gap-1 type-caption text-fg-muted">
        {label}
        {info && <InfoTip content={info} />}
      </dt>
      <dd className="type-title-sm tabular-nums text-fg">{value}</dd>
    </div>
  );
}

/**
 * The explainer: the score, every component with its weight, points and raw inputs, the
 * requirements with progress, and the 90-day stats and curve. For the person and staff only.
 */
export function ReputationDetail({ rep }: { rep: Reputation }) {
  const total = rep.components.reduce((s, c) => s + (c.scored ? c.points : 0), 0);
  const s = rep.stats;
  return (
    <div className="flex flex-col gap-section">
      <Overview rep={rep} />

      <Section
        title="How the score is made"
        info="Each part scores 0 to 100. A part's weight is its share of the parts that have data; parts without data give their weight to the rest. The score is the sum of the points, rounded."
      >
        <ul className="flex flex-col">
          {rep.components.map((c) => (
            <ComponentItem key={c.key} c={c} />
          ))}
          <li className="flex items-center gap-2 pt-4">
            <span className="type-label font-semibold text-fg">Total</span>
            <span className="ml-auto type-label tabular-nums text-fg">
              {total.toFixed(1)} pts{rep.status === "rated" && rep.score !== null ? ` → ${rep.score}` : ""}
            </span>
          </li>
        </ul>
      </Section>

      <Section title="Requirements" info="A score shows once all of these are met.">
        <ListGroup>
          {rep.requirements.map((q) => (
            <ListRow
              key={q.key}
              leading={q.met ? <CheckCircle2 className="size-5 text-success-strong" aria-label="Met" /> : <Circle className="size-5 text-fg-muted" aria-label="Not met" />}
              title={q.label}
              trailing={<span className="type-label tabular-nums text-fg">{q.progress}</span>}
            />
          ))}
        </ListGroup>
      </Section>

      <Section title="Last 90 days" info="Returns are time-weighted: money put in or taken out never counts as a gain or a loss, so a deposit can't hide a drawdown.">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
          <Stat label="Return, 30 days" value={formatReputationPct(s.return_30d_pct)} />
          <Stat label="Return, 90 days" value={formatReputationPct(s.return_90d_pct)} />
          <Stat label="Gain, 30 days" value={formatReputationAmount(s.gain_30d)} info="Value change minus money put in or taken out." />
          <Stat label="Max drawdown" value={formatReputationPct(s.max_drawdown_90d_pct, false)} />
          <Stat label="Volume, 30 days" value={formatReputationAmount(s.volume_30d)} />
          <Stat label="Activity, 30 days" value={String(s.activity_30d)} />
          <Stat label="Active days" value={String(s.active_days_90d)} />
          <Stat label="Incidents" value={String(s.incidents_90d)} />
        </dl>
        <ReputationEquityChart points={s.equity} />
      </Section>
    </div>
  );
}
