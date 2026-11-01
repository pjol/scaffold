"use client";

import { reposHighlightLines, reposTokenRole, type ReposToken, type ReposTokenRole } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { memo, useMemo, type ReactNode } from "react";

/** Token colours: roles map onto semantic colour tokens, so a restyle recolours code too. */
export const TOKEN_CLASS: Record<ReposTokenRole, string> = {
  keyword: "text-primary-strong",
  string: "text-success-strong",
  number: "text-warning-strong",
  comment: "text-fg-muted italic",
  function: "text-info-strong",
  type: "text-accent-strong",
  attr: "text-warning-strong",
  meta: "text-danger-strong",
  deletion: "text-danger-strong",
  addition: "text-success-strong",
  plain: "",
};

/** One line's tokens as spans. */
export const TokenLine = memo(function TokenLine({ tokens }: { tokens: ReposToken[] }) {
  if (tokens.length === 0) return <>{"​"}</>;
  return (
    <>
      {tokens.map((t, i) => {
        const cls = TOKEN_CLASS[reposTokenRole(t.scope)];
        return cls ? (
          <span key={i} className={cls}>
            {t.text}
          </span>
        ) : (
          <span key={i}>{t.text}</span>
        );
      })}
    </>
  );
});

/** Highlight `code` once per change. */
export function useHighlighted(code: string, language: string): ReposToken[][] {
  return useMemo(() => reposHighlightLines(code, language), [code, language]);
}

export interface CodeViewProps {
  code: string;
  language: string;
  /** Selected lines (1-based, inclusive): highlighted and scrolled to. */
  selected?: { from: number; to: number } | null;
  /** Clicking a line number; shift extends a range. */
  onSelectLine?: (line: number, extend: boolean) => void;
  /** Something before each line's code (blame), by line number. */
  gutter?: (line: number) => ReactNode;
  wrap?: boolean;
  /** Line numbers (off for snippets such as README code blocks). @default true */
  numbers?: boolean;
  className?: string;
}

/** A file with line numbers and syntax colours. Line numbers are links (#L12) for sharing. */
export function CodeView({ code, language, selected, onSelectLine, gutter, wrap, numbers = true, className }: CodeViewProps) {
  const lines = useHighlighted(code, language);
  return (
    <div className={cn("overflow-x-auto rounded-b-lg font-mono text-label font-normal", className)}>
      <table className="w-full border-collapse">
        <tbody>
          {lines.map((tokens, i) => {
            const n = i + 1;
            const on = !!selected && n >= selected.from && n <= selected.to;
            return (
              <tr key={n} id={`L${n}`} className={cn("align-top", on && "bg-warning-soft")}>
                {gutter ? <td className="border-r border-border px-2 align-top">{gutter(n)}</td> : null}
                {numbers ? <td className="w-1 select-none px-0 text-right align-top">
                  <a
                    href={`#L${n}`}
                    aria-label={`Line ${n}`}
                    onClick={(e) => {
                      if (!onSelectLine) return;
                      e.preventDefault();
                      onSelectLine(n, e.shiftKey);
                    }}
                    className={cn(
                      "block min-w-12 px-3 text-fg-subtle tabular-nums hover:text-fg focus-visible:outline-2 focus-visible:outline-ring",
                      on && "text-fg",
                    )}
                  >
                    {n}
                  </a>
                </td> : null}
                <td className={cn("px-3 text-fg", wrap ? "whitespace-pre-wrap [overflow-wrap:anywhere]" : "whitespace-pre")}>
                  <TokenLine tokens={tokens} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
