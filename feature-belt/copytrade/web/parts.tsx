"use client";

import { copytradeStatusLabel, formatCopytradePct, type CopytradePerson, type CopytradeStatus } from "@scaffoldapp/shared";
import { Avatar, Badge, cn } from "@scaffoldapp/ui-web";
import { Fragment, type ReactNode } from "react";
import { signTone } from "@/features/exchange/parts";

export { Stat, signTone, useExchangeMoney } from "@/features/exchange/parts";

/** A copy's status as a badge. */
export function CopyStatusBadge({ status }: { status: CopytradeStatus }) {
  const tone = status === "active" ? "success" : status === "paused" ? "warning" : "neutral";
  return (
    <Badge tone={tone} size="sm">
      {copytradeStatusLabel(status)}
    </Badge>
  );
}

/** A signed percentage coloured by direction ("—" when unknown). */
export function PctText({ pct, signed = true, className }: { pct: string | null | undefined; signed?: boolean; className?: string }) {
  return <span className={cn("tabular-nums", signed && signTone(pct), className)}>{formatCopytradePct(pct, signed)}</span>;
}

/** A person: avatar and display name. */
export function PersonLine({ person, size = "sm", children }: { person: CopytradePerson; size?: "sm" | "md"; children?: ReactNode }) {
  return (
    <span className="flex min-w-0 items-center gap-3">
      <Avatar src={person.avatar_url} name={person.display_name} size={size} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate type-label font-semibold text-fg">{person.display_name}</span>
        {children}
      </span>
    </span>
  );
}

/** The risk disclosure (simple Markdown: headings, paragraphs, bullets, **bold**). */
export function Disclosure({ markdown, className }: { markdown: string; className?: string }) {
  const blocks: { kind: "h" | "p" | "li"; text: string }[] = [];
  for (const raw of markdown.split("\n")) {
    const line = raw.trimEnd();
    if (!line.trim()) continue;
    if (line.startsWith("## ")) blocks.push({ kind: "h", text: line.slice(3) });
    else if (line.startsWith("- ")) blocks.push({ kind: "li", text: line.slice(2) });
    else if (/^\s+/.test(line) && blocks.length > 0) {
      const last = blocks[blocks.length - 1];
      if (last) last.text += ` ${line.trim()}`;
    } else blocks.push({ kind: "p", text: line });
  }
  const inline = (text: string) =>
    text.split(/(\*\*[^*]+\*\*)/).map((part, i) =>
      part.startsWith("**") ? (
        <strong key={i} className="font-semibold text-fg">
          {part.slice(2, -2)}
        </strong>
      ) : (
        <Fragment key={i}>{part}</Fragment>
      ),
    );
  return (
    <div className={cn("flex flex-col gap-2 type-body text-fg-muted", className)}>
      {blocks.map((b, i) =>
          b.kind === "h" ? (
            <p key={i} className="type-label font-semibold text-fg">
              {b.text}
            </p>
          ) : b.kind === "li" ? (
            <p key={i} className="flex gap-2">
              <span aria-hidden>•</span>
              <span>{inline(b.text)}</span>
            </p>
          ) : (
            <p key={i}>{inline(b.text)}</p>
          ),
      )}
    </div>
  );
}

/** "Mar 4, 14:05". */
export function when(iso: string) {
  return new Date(iso).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}
