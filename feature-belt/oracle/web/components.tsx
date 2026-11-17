"use client";

import {
  ORACLE_KIND_LABEL,
  ORACLE_STATUS_INFO,
  ORACLE_STATUS_LABEL,
  ORACLE_VERDICT_LABEL,
  formatOraclePremium,
  formatOraclePrice,
  oracleAreaPath,
  oraclePath,
  oracleScale,
  oracleStatusTone,
  oracleVerdictTone,
  useNow,
  type OracleFeedPart,
  type OracleFeedSource,
  type OracleObservation,
  type OracleSeriesPoint,
  type OracleStatus,
  type OracleVerdict,
} from "@scaffoldapp/shared";
import { Badge, InfoTip, ListGroup, ListRow } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { formatDateTime, formatRelative } from "@/lib/format";
import { oraclePaths } from "./nav";

export function OracleStatusBadge({ status, size = "sm" }: { status: OracleStatus; size?: "sm" | "md" }) {
  return (
    <span className="inline-flex items-center gap-1">
      <Badge tone={oracleStatusTone(status)} size={size} dot>
        {ORACLE_STATUS_LABEL[status]}
      </Badge>
      <InfoTip content={ORACLE_STATUS_INFO[status]} label={`What ${ORACLE_STATUS_LABEL[status].toLowerCase()} means`} />
    </span>
  );
}

export function OracleVerdictBadge({ verdict }: { verdict: OracleVerdict }) {
  return (
    <Badge tone={oracleVerdictTone(verdict)} size="sm">
      {ORACLE_VERDICT_LABEL[verdict]}
    </Badge>
  );
}

/**
 * A price line: the series as a path in the primary colour over a soft area, with the
 * high and low printed at the edges. Draws nothing for an empty series.
 */
export function OraclePriceChart({ points, label, height = 200 }: { points: OracleSeriesPoint[]; label: string; height?: number }) {
  const W = 600;
  const { xy, min, max } = oracleScale(points, W, height, 8);
  if (xy.length === 0) return null;
  const line = oraclePath(xy);
  const area = oracleAreaPath(xy, height);
  const fmt = (v: number) => v.toLocaleString("en-US", { maximumFractionDigits: v >= 100 ? 2 : 6 });
  return (
    <figure className="relative flex flex-col gap-1">
      <div className="flex justify-between type-caption text-fg-muted tabular-nums">
        <span>High {fmt(max)}</span>
      </div>
      <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" role="img" aria-label={label} className="h-48 w-full overflow-visible sm:h-56">
        <path d={area} className="fill-primary-soft" />
        <path d={line} fill="none" className="stroke-primary" strokeWidth={2} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
      </svg>
      <div className="flex justify-between type-caption text-fg-muted tabular-nums">
        <span>Low {fmt(min)}</span>
      </div>
    </figure>
  );
}

function Reasons({ o }: { o: OracleObservation }) {
  return (
    <div className="flex flex-col gap-1">
      <span>
        Score {o.score.toFixed(2)} · weight {o.weight} · {o.model}
      </span>
      {o.reasons.map((r, i) => (
        <span key={i}>{r}</span>
      ))}
    </div>
  );
}

/**
 * Screened observations: a table from 768 px, rows below. `withFeed` adds the feed
 * column (streams across feeds).
 */
export function OracleObservationList({ items, quote, withFeed = false }: { items: OracleObservation[]; quote?: string; withFeed?: boolean }) {
  const now = useNow(30_000);
  return (
    <>
      <div className="hidden md:block">
        <Table label="Observations">
          <THead>
            <TH>Received</TH>
            {withFeed && <TH>Feed</TH>}
            <TH>Venue</TH>
            <TH className="hidden lg:table-cell">Kind</TH>
            <TH align="right" info="As reported; after a venue premium the normalised price follows the arrow.">
              Price
            </TH>
            <TH>Verdict</TH>
            <TH align="right" info="Manipulation score from 0 to 1. Below 0.35 accepted; 0.35 to 0.70 downweighted; above 0.70 rejected.">
              Score
            </TH>
            <TH className="w-full">Reasons</TH>
          </THead>
          <tbody>
            {items.map((o) => (
              <TR key={o.id}>
                <TD className="whitespace-nowrap text-fg-muted">
                  <time dateTime={o.received_at} title={formatDateTime(o.received_at)}>
                    {formatRelative(o.received_at, now)}
                  </time>
                </TD>
                {withFeed && (
                  <TD className="whitespace-nowrap">
                    <Link href={oraclePaths.feed(o.feed_key)} className="font-semibold text-fg underline-offset-2 hover:underline">
                      {o.feed_key}
                    </Link>
                  </TD>
                )}
                <TD className="whitespace-nowrap">{o.venue}</TD>
                <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell">{ORACLE_KIND_LABEL[o.kind]}</TD>
                <TD align="right" className="whitespace-nowrap tabular-nums">
                  {formatOraclePrice(o.raw_price)}
                  {o.premium !== 0 && <span className="text-fg-muted"> → {formatOraclePrice(o.price)}</span>}
                  {quote ? <span className="text-fg-muted"> {quote}</span> : null}
                </TD>
                <TD className="whitespace-nowrap">
                  <OracleVerdictBadge verdict={o.verdict} />
                </TD>
                <TD align="right" className="tabular-nums">
                  {o.score.toFixed(2)}
                </TD>
                <TD className="max-w-0">
                  <span className="flex items-center gap-1">
                    <span className="truncate text-fg-muted">{o.reasons.join(" · ")}</span>
                    <InfoTip content={<Reasons o={o} />} label="Why this verdict" />
                  </span>
                </TD>
              </TR>
            ))}
          </tbody>
        </Table>
      </div>
      <div className="md:hidden">
        <ListGroup>
          <ul className="flex flex-col">
            {items.map((o) => (
              <li key={o.id}>
                <ListRow
                  title={
                    <span className="tabular-nums">
                      {withFeed ? `${o.feed_key} · ` : ""}
                      {formatOraclePrice(o.raw_price)} · {o.venue}
                    </span>
                  }
                  info={<Reasons o={o} />}
                  trailing={<OracleVerdictBadge verdict={o.verdict} />}
                />
              </li>
            ))}
          </ul>
        </ListGroup>
      </div>
    </>
  );
}

/** A single's venues: last price, when, health and premium. */
export function OracleSourcesGroup({ sources }: { sources: OracleFeedSource[] }) {
  const now = useNow(30_000);
  return (
    <ListGroup title="Venues" info="Each venue's latest print and the health of its last fetch. A premium is removed from a venue whose prices run above or below the others.">
      <ul className="flex flex-col">
        {sources.map((s) => (
          <li key={s.venue}>
            <ListRow
              title={s.venue}
              info={
                <div className="flex flex-col gap-1">
                  <span>Source: {s.source}</span>
                  {s.note && <span>{s.note}</span>}
                  <span>
                    Premium {formatOraclePremium(s.premium)}
                    {s.premium !== 0 ? (s.premium_learned ? " (learned)" : " (fixed)") : ""} · weight {s.weight}
                  </span>
                  {s.last_at && <span>Last print {formatDateTime(s.last_at)}</span>}
                </div>
              }
              trailing={
                <span className="flex items-center gap-3">
                  <span className="tabular-nums text-fg">{formatOraclePrice(s.last_price)}</span>
                  <span className="hidden text-fg-muted sm:inline">{s.last_at ? formatRelative(s.last_at, now) : "No prints"}</span>
                  <Badge tone={s.ok ? "success" : "warning"} size="sm" dot>
                    {s.ok ? "OK" : "Failing"}
                  </Badge>
                </span>
              }
            />
          </li>
        ))}
      </ul>
    </ListGroup>
  );
}

/** A basket's parts with their prices; each opens its feed. */
export function OraclePartsGroup({ parts }: { parts: OracleFeedPart[] }) {
  return (
    <ListGroup title="Parts" info="The basket's price is the sum of quantity × price over its parts. Its status is the worst of theirs.">
      <ul className="flex flex-col">
        {parts.map((p) => (
          <li key={p.feed_id}>
            <ListRow
              asChild
              chevron
              title={`${p.quantity} × ${p.name}`}
              trailing={
                <span className="flex items-center gap-3">
                  <span className="tabular-nums text-fg">{formatOraclePrice(p.price)}</span>
                  <Badge tone={oracleStatusTone(p.status)} size="sm" dot>
                    {ORACLE_STATUS_LABEL[p.status]}
                  </Badge>
                </span>
              }
            >
              <Link href={oraclePaths.feed(p.key)} />
            </ListRow>
          </li>
        ))}
      </ul>
    </ListGroup>
  );
}
