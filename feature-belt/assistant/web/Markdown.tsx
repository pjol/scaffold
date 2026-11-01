"use client";

import { parseMarkdown, type MdBlock, type MdInline } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { Fragment, memo, useMemo, type ReactNode } from "react";

/**
 * Renders an assistant reply from the shared Markdown tree as plain elements and text
 * nodes: no HTML from the model is ever parsed, and links are limited to http(s) and
 * mailto (opened in a new tab, no referrer).
 */
export const Markdown = memo(function Markdown({ text, className }: { text: string; className?: string }) {
  const blocks = useMemo(() => parseMarkdown(text), [text]);
  return <div className={cn("flex min-w-0 flex-col gap-3 type-body text-fg [overflow-wrap:anywhere]", className)}>{blocks.map((b, i) => <Block key={i} block={b} />)}</div>;
});

function Block({ block }: { block: MdBlock }) {
  switch (block.type) {
    case "paragraph":
      return <p className="whitespace-pre-wrap">{inline(block.children)}</p>;
    case "heading":
      return <p className={cn("font-semibold", block.level === 1 ? "type-title-sm" : "type-body")}>{inline(block.children)}</p>;
    case "list": {
      const items = block.items.map((item, i) => (
        <li key={i} className="whitespace-pre-wrap pl-1">
          {inline(item)}
        </li>
      ));
      return block.ordered ? (
        <ol start={block.start} className="flex list-decimal flex-col gap-1 pl-6 marker:text-fg-muted">
          {items}
        </ol>
      ) : (
        <ul className="flex list-disc flex-col gap-1 pl-6 marker:text-fg-muted">{items}</ul>
      );
    }
    case "quote":
      return <blockquote className="rounded-md bg-sunken px-4 py-3 whitespace-pre-wrap text-fg-muted">{inline(block.children)}</blockquote>;
    case "code":
      return (
        <pre className="overflow-x-auto rounded-md bg-sunken px-4 py-3 type-label">
          <code className="font-mono">{block.text}</code>
        </pre>
      );
    case "rule":
      return <hr className="border-border" />;
  }
}

function inline(nodes: MdInline[]): ReactNode {
  return nodes.map((n, i) => <Fragment key={i}>{inlineNode(n)}</Fragment>);
}

function inlineNode(n: MdInline): ReactNode {
  switch (n.type) {
    case "text":
      return n.text;
    case "strong":
      return <strong className="font-semibold">{inline(n.children)}</strong>;
    case "em":
      return <em>{inline(n.children)}</em>;
    case "code":
      return <code className="rounded-xs bg-sunken px-1 py-0.5 font-mono">{n.text}</code>;
    case "link":
      return (
        <a href={n.href} target="_blank" rel="noopener noreferrer nofollow" className="text-primary-strong underline underline-offset-2 hover:no-underline">
          {inline(n.children)}
        </a>
      );
  }
}
