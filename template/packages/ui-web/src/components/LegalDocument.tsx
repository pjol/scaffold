"use client";

import { ChevronDown, Link2 } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Fragment, useEffect, useMemo, useState, type MouseEvent, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { Badge } from "./Badge";
import { InfoTip } from "./InfoTip";

export type LegalBlock = { type: "paragraph"; text: string } | { type: "bullets"; items: string[] };
export interface LegalSection {
  id: string;
  heading: string;
  blocks: LegalBlock[];
}
/** A versioned legal document; structurally `LegalDocument` from `@scaffoldapp/shared/legal`. */
export interface LegalDocumentData {
  version: string;
  title: string;
  /** `YYYY-MM-DD` or display text. */
  effective: string;
  sections: LegalSection[];
}

export interface LegalDocumentProps {
  document: LegalDocumentData;
  /** Contents list: sticky sidebar in wide containers, collapsible above the text in narrow ones. @default true */
  toc?: boolean;
  /** Render the title (hide it inside a Sheet that already shows it). @default true */
  showTitle?: boolean;
  /** Number the sections. @default true */
  numbered?: boolean;
  /** Prefix for section anchors (unique ids when two documents share a page). */
  idPrefix?: string;
  /** Write `#anchor` into the URL when a contents link is used. @default false */
  updateHash?: boolean;
  className?: string;
}

/** "2026-10-01" → "October 1, 2026" (a calendar date, not an instant). */
export function formatEffectiveDate(effective: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(effective.trim());
  if (!m) return effective;
  const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
  return d.toLocaleDateString("en-US", { year: "numeric", month: "long", day: "numeric", timeZone: "UTC" });
}

const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");

const LINK_RE = /(https?:\/\/[^\s)]+[^\s).,;:!?]|[\w.+-]+@[\w-]+\.[\w.-]*\w)/g;

/** Plain text with URLs and emails turned into links. */
function Linkified({ text }: { text: string }) {
  const parts = text.split(LINK_RE);
  return (
    <>
      {parts.map((part, k) =>
        k % 2 === 1 ? (
          <a
            key={k}
            href={part.includes("@") && !part.startsWith("http") ? `mailto:${part}` : part}
            className="font-medium text-primary-strong underline underline-offset-2"
            {...(part.startsWith("http") ? { target: "_blank", rel: "noreferrer noopener" } : {})}
          >
            {part}
          </a>
        ) : (
          <Fragment key={k}>{part}</Fragment>
        ),
      )}
    </>
  );
}

/**
 * Readable legal text: 65ch measure, numbered headings with anchors, bullets, and a
 * contents list that follows the section being read. Works in a page or inside a Sheet
 * (container queries, not viewport breakpoints).
 */
export function LegalDocument({ document: doc, toc = true, showTitle = true, numbered = true, idPrefix, updateHash = false, className }: LegalDocumentProps) {
  const t = useMotionTokens();
  const reduce = useReducedMotion();
  const prefix = idPrefix ?? slug(doc.title || "doc");
  const ids = useMemo(() => doc.sections.map((s) => `${prefix}-${s.id}`), [doc.sections, prefix]);
  const [active, setActive] = useState<string | null>(ids[0] ?? null);
  const [tocOpen, setTocOpen] = useState(false);

  useEffect(() => {
    if (!toc || typeof IntersectionObserver === "undefined") return undefined;
    const els = ids.map((id) => globalThis.document.getElementById(id)).filter((el): el is HTMLElement => el !== null);
    if (!els.length) return undefined;
    const io = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActive(visible[0].target.id);
      },
      { rootMargin: "-12% 0px -70% 0px", threshold: 0 },
    );
    els.forEach((el) => io.observe(el));
    return () => io.disconnect();
  }, [ids, toc]);

  const jump = (e: MouseEvent<HTMLAnchorElement>, id: string) => {
    const el = globalThis.document.getElementById(id);
    if (!el) return;
    e.preventDefault();
    el.scrollIntoView({ behavior: reduce ? "auto" : "smooth", block: "start" });
    el.focus({ preventScroll: true });
    setActive(id);
    setTocOpen(false);
    if (updateHash) history.replaceState(null, "", `#${id}`);
  };

  const label = (k: number, heading: string) => (numbered ? `${k + 1}. ${heading}` : heading);

  const tocList = (variant: "inline" | "side") => (
    <ol className="flex flex-col gap-0.5">
      {doc.sections.map((s, k) => {
        const id = ids[k] ?? s.id;
        const on = active === id;
        return (
          <li key={s.id}>
            <a
              href={`#${id}`}
              onClick={(e) => jump(e, id)}
              aria-current={on ? "location" : undefined}
              className={cn(
                "relative flex min-h-control items-center rounded-sm py-2 pr-2 pl-4 type-label transition-colors pointer-coarse:min-h-tap",
                on ? "font-semibold text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
              )}
            >
              {on && <motion.span layoutId={`${prefix}-toc-${variant}`} transition={t.spring.snappy} className="absolute top-2 bottom-2 left-0 w-0.5 rounded-pill bg-primary" />}
              {label(k, s.heading)}
            </a>
          </li>
        );
      })}
    </ol>
  );

  const body = (
    <div className="flex min-w-0 max-w-prose flex-col gap-8">
      {doc.sections.map((s, k) => {
        const id = ids[k] ?? s.id;
        return (
          <section key={s.id} aria-labelledby={id} className="flex scroll-mt-24 flex-col gap-3">
            <h2 id={id} tabIndex={-1} className="group flex scroll-mt-24 items-baseline gap-2 type-title-sm text-fg outline-none">
              <span className="text-balance">{label(k, s.heading)}</span>
              <a
                href={`#${id}`}
                onClick={(e) => jump(e, id)}
                aria-label={`Link to ${s.heading}`}
                className="text-fg-muted opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100 [&_svg]:size-4"
              >
                <Link2 />
              </a>
            </h2>
            <div className="flex flex-col gap-4 type-body text-fg">
              {s.blocks.map((b, j) =>
                b.type === "paragraph" ? (
                  <p key={j} className="text-pretty">
                    <Linkified text={b.text} />
                  </p>
                ) : (
                  <ul key={j} className="flex list-disc flex-col gap-2 pl-6 marker:text-fg-muted">
                    {b.items.map((item, n) => (
                      <li key={n} className="pl-1 text-pretty">
                        <Linkified text={item} />
                      </li>
                    ))}
                  </ul>
                ),
              )}
            </div>
          </section>
        );
      })}
    </div>
  );

  const meta = (
    <div className="flex items-center gap-2">
      <Badge tone="neutral" size="md">
        Effective {formatEffectiveDate(doc.effective)}
      </Badge>
      <InfoTip content={`Version ${doc.version}`} label="Document version" />
    </div>
  );

  const header: ReactNode = showTitle ? (
    <header className="flex flex-col gap-3">
      <h1 className="type-title text-balance text-fg lg:type-display">{doc.title}</h1>
      {meta}
    </header>
  ) : (
    meta
  );

  if (!toc || doc.sections.length < 2) {
    return (
      <article className={cn("flex flex-col gap-8", className)}>
        {header}
        {body}
      </article>
    );
  }

  return (
    <article className={cn("@container flex flex-col gap-8", className)}>
      {header}
      <div className="flex flex-col gap-6 @3xl:grid @3xl:grid-cols-[minmax(12rem,16rem)_minmax(0,1fr)] @3xl:gap-12">
        <nav aria-label="Contents">
          <div className="rounded-lg border border-border bg-surface @3xl:hidden">
            <button
              type="button"
              aria-expanded={tocOpen}
              onClick={() => setTocOpen((o) => !o)}
              className="flex min-h-tap w-full items-center justify-between gap-3 px-4 type-body font-semibold text-fg"
            >
              Contents
              <ChevronDown className={cn("size-4 text-fg-muted transition-transform spring-snappy", tocOpen && "rotate-180")} />
            </button>
            <AnimatePresence initial={false}>
              {tocOpen && (
                <motion.div initial={{ height: 0, opacity: 0 }} animate={{ height: "auto", opacity: 1 }} exit={{ height: 0, opacity: 0 }} transition={t.spring.gentle} className="overflow-hidden">
                  <div className="border-t border-border p-2">{tocList("inline")}</div>
                </motion.div>
              )}
            </AnimatePresence>
          </div>
          <div className="sticky top-24 hidden max-h-[calc(100dvh-8rem)] flex-col gap-2 overflow-y-auto @3xl:flex">
            <p className="pl-4 type-caption tracking-wide text-fg-muted uppercase">Contents</p>
            {tocList("side")}
          </div>
        </nav>
        {body}
      </div>
    </article>
  );
}
