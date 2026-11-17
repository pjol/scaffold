"use client";

import { docsTimeAgo, docsWordCount, useDocsAnalytics, type DocsBlock, type DocsPageDetail } from "@scaffoldapp/shared";
import { Avatar, Dialog, ErrorState, SegmentedControl, Skeleton, SkeletonRow, cn } from "@scaffoldapp/ui-web";
import { BadgeCheck } from "lucide-react";
import { useState } from "react";

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-2">
      <dt className="type-label text-fg-muted">{label}</dt>
      <dd className="text-right type-body text-fg">{children}</dd>
    </div>
  );
}

/** Daily views as thin bars (one series, so no legend) with a tooltip per day and a table for screen readers. */
function ViewsChart({ days }: { days: { date: string; views: number; viewers: number }[] }) {
  const [hover, setHover] = useState<number | null>(null);
  const top = Math.max(1, ...days.map((d) => d.views));
  const fmt = (d: string) => new Date(`${d}T00:00:00`).toLocaleDateString(undefined, { month: "short", day: "numeric" });
  const h = hover !== null ? days[hover] : null;
  return (
    <figure className="flex flex-col gap-2">
      <div className="relative">
        <div className="flex h-28 items-end gap-0.5 border-b border-border" aria-hidden onMouseLeave={() => setHover(null)}>
          {days.map((d, i) => (
            <div key={d.date} className="flex h-full flex-1 items-end" onMouseEnter={() => setHover(i)}>
              <div
                className={cn("w-full rounded-t-xs transition-colors motion-reduce:transition-none", d.views ? (hover === i ? "bg-primary-strong" : "bg-primary") : "bg-transparent")}
                style={{ height: d.views ? `${Math.max(4, (d.views / top) * 100)}%` : 0 }}
              />
            </div>
          ))}
        </div>
        {h ? (
          <div className="pointer-events-none absolute -top-2 right-0 rounded-sm bg-inverse px-2 py-1 type-caption text-fg-inverse shadow-md">
            {fmt(h.date)}: {h.views} {h.views === 1 ? "view" : "views"}, {h.viewers} {h.viewers === 1 ? "person" : "people"}
          </div>
        ) : null}
      </div>
      <figcaption className="flex justify-between type-caption text-fg-muted">
        <span>{days[0] ? fmt(days[0].date) : ""}</span>
        <span>Views per day</span>
        <span>{days.length ? fmt(days[days.length - 1]!.date) : ""}</span>
      </figcaption>
      <table className="sr-only">
        <caption>Views per day</caption>
        <thead>
          <tr>
            <th>Day</th>
            <th>Views</th>
            <th>People</th>
          </tr>
        </thead>
        <tbody>
          {days.map((d) => (
            <tr key={d.date}>
              <td>{fmt(d.date)}</td>
              <td>{d.views}</td>
              <td>{d.viewers}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}

function Analytics({ pageId }: { pageId: string }) {
  const [days, setDays] = useState<"7" | "28" | "90">("28");
  const a = useDocsAnalytics(pageId, Number(days));
  return (
    <section className="flex flex-col gap-3 border-t border-border pt-4" aria-label="Views">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="type-title-sm text-fg">Views</h3>
        <SegmentedControl<"7" | "28" | "90">
          value={days}
          onValueChange={setDays}
          fullWidth={false}
          aria-label="Period"
          options={[
            { value: "7", label: "7 days" },
            { value: "28", label: "28 days" },
            { value: "90", label: "90 days" },
          ]}
        />
      </div>
      {a.isPending ? (
        <Skeleton className="h-36 w-full" />
      ) : a.isError ? (
        <ErrorState title="Couldn't load views." onRetry={() => void a.refetch()} />
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3">
            <div className="rounded-md bg-sunken p-3">
              <p className="type-caption text-fg-muted">Views</p>
              <p className="type-headline text-fg">{a.data.views.toLocaleString()}</p>
            </div>
            <div className="rounded-md bg-sunken p-3">
              <p className="type-caption text-fg-muted">People</p>
              <p className="type-headline text-fg">{a.data.viewers.toLocaleString()}</p>
            </div>
          </div>
          <ViewsChart days={a.data.days} />
          {a.data.people.length ? (
            <ul className="flex max-h-48 flex-col divide-y divide-border overflow-y-auto" aria-label="Who viewed it">
              {a.data.people.map((p) => (
                <li key={p.user.id} className="flex items-center gap-2 py-2">
                  <Avatar size="xs" name={p.user.name} {...(p.user.avatar_url ? { src: p.user.avatar_url } : {})} />
                  <span className="min-w-0 flex-1 truncate type-label text-fg">{p.user.name}</span>
                  <span className="type-caption text-fg-muted">
                    {p.views} · {docsTimeAgo(p.last_viewed_at)}
                  </span>
                </li>
              ))}
              {a.data.others > 0 ? <li className="py-2 type-caption text-fg-muted">and {a.data.others} more</li> : null}
            </ul>
          ) : (
            <p className="type-label text-fg-muted">Nobody has opened it in this period.</p>
          )}
        </>
      )}
    </section>
  );
}

/** Page info: words, reading time, who made and last edited it, verification and (for editors) views. */
export function PageInfoDialog({ open, onOpenChange, detail, blocks }: { open: boolean; onOpenChange: (open: boolean) => void; detail: DocsPageDetail; blocks: readonly DocsBlock[] }) {
  const page = detail.page;
  const counts = docsWordCount(blocks);
  const who = (id: string | null | undefined) => (id ? (detail.refs.users[id]?.name ?? "Someone") : "Someone");
  const canEdit = page.my_level === "edit" || page.my_level === "manage";
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Page info">
      {open ? (
        <div className="flex flex-col gap-2">
          <dl className="flex flex-col divide-y divide-border">
            {page.kind === "page" ? (
              <>
                <Fact label="Words">{counts.words.toLocaleString()}</Fact>
                <Fact label="Characters">{counts.characters.toLocaleString()}</Fact>
                <Fact label="Reading time">{counts.minutes} min</Fact>
              </>
            ) : null}
            <Fact label="Created">
              {who(page.created_by)}, {new Date(page.created_at).toLocaleDateString()}
            </Fact>
            <Fact label="Last edited">
              {who(page.updated_by)}, {docsTimeAgo(page.updated_at)}
            </Fact>
            {page.verified ? (
              <Fact label="Verified">
                <span className="inline-flex items-center gap-1">
                  <BadgeCheck aria-hidden className="size-4 text-success" />
                  {who(page.verified_by)}
                  {page.verified_until ? `, until ${new Date(page.verified_until).toLocaleDateString()}` : ""}
                </span>
              </Fact>
            ) : null}
          </dl>
          {canEdit ? <Analytics pageId={page.id} /> : null}
        </div>
      ) : (
        <SkeletonRow />
      )}
    </Dialog>
  );
}
