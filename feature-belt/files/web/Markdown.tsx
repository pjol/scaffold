import { filesParseMarkdown, type FilesMdBlock, type FilesMdInline } from "@scaffoldapp/shared";
import { Fragment, useMemo, type ReactNode } from "react";

function Inline({ nodes }: { nodes: FilesMdInline[] }): ReactNode {
  return nodes.map((n, i) => {
    switch (n.type) {
      case "text":
        return <Fragment key={i}>{n.text}</Fragment>;
      case "code":
        return (
          <code key={i} className="rounded-xs bg-sunken px-1 font-mono">
            {n.text}
          </code>
        );
      case "strong":
        return (
          <strong key={i} className="font-semibold">
            <Inline nodes={n.children} />
          </strong>
        );
      case "em":
        return (
          <em key={i}>
            <Inline nodes={n.children} />
          </em>
        );
      case "link":
        return (
          <a key={i} href={n.href} target="_blank" rel="noopener noreferrer nofollow" className="text-primary-strong underline underline-offset-2">
            <Inline nodes={n.children} />
          </a>
        );
    }
  });
}

const HEADING = ["type-headline", "type-title", "type-title-sm", "type-body-lg font-semibold", "type-body font-semibold", "type-body font-semibold"];

function Block({ block }: { block: FilesMdBlock }) {
  switch (block.type) {
    case "heading": {
      const Tag = `h${block.level}` as "h1";
      return (
        <Tag className={`${HEADING[block.level - 1]} text-fg [&:not(:first-child)]:mt-4`}>
          <Inline nodes={block.children} />
        </Tag>
      );
    }
    case "paragraph":
      return (
        <p className="type-body text-fg">
          <Inline nodes={block.children} />
        </p>
      );
    case "list": {
      const Tag = block.ordered ? "ol" : "ul";
      return (
        <Tag className={`flex flex-col gap-1 pl-6 type-body text-fg ${block.ordered ? "list-decimal" : "list-disc"}`} start={block.ordered ? block.start : undefined}>
          {block.items.map((item, i) => (
            <li key={i}>
              <Inline nodes={item} />
            </li>
          ))}
        </Tag>
      );
    }
    case "quote":
      return (
        <blockquote className="rounded-md bg-sunken px-4 py-3 type-body text-fg-muted">
          <Inline nodes={block.children} />
        </blockquote>
      );
    case "code":
      return (
        <pre className="overflow-x-auto rounded-md bg-sunken p-4 font-mono type-label text-fg">
          <code>{block.text}</code>
        </pre>
      );
    case "table":
      return (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full border-collapse type-label">
            <thead className="bg-sunken">
              <tr>
                {block.header.map((c, i) => (
                  <th key={i} className="px-3 py-2 text-left font-semibold text-fg">
                    <Inline nodes={c} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, r) => (
                <tr key={r} className="border-t border-border">
                  {row.map((c, i) => (
                    <td key={i} className="px-3 py-2 text-fg">
                      <Inline nodes={c} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "rule":
      return <hr className="border-border" />;
  }
}

/** Renders Markdown as components (never HTML from the file). */
export function FilesMarkdown({ text }: { text: string }) {
  const blocks = useMemo(() => filesParseMarkdown(text), [text]);
  return (
    <article className="flex max-w-prose flex-col gap-3">
      {blocks.map((b, i) => (
        <Block key={i} block={b} />
      ))}
    </article>
  );
}
