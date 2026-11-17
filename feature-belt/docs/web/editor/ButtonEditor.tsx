"use client";

import { docsBlocksToMarkdown, docsSafeLink, docsTitle, useDocsSearch, type DocsBlock, type DocsBlockButtonAction, type DocsBlockButtonBlock, type DocsValue } from "@scaffoldapp/shared";
import { Button, Input, TextArea, cn } from "@scaffoldapp/ui-web";
import { Database, Link2, ListPlus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Floating } from "../Floating";
import { buttonBlocksFromMarkdown } from "./blocks";

type Draft = { kind: "insert"; markdown: string } | { kind: "link"; url: string } | { kind: "add_row"; database_id: string; title: string; values: Record<string, DocsValue> };

function asMarkdown(blocks: DocsBlockButtonBlock[]): string {
  // Template blocks as Markdown for editing (ids only matter for the renderer).
  const flat: DocsBlock[] = [];
  let n = 0;
  const walk = (list: DocsBlockButtonBlock[], parent: string | null) => {
    for (const b of list) {
      const id = `t${n++}`;
      flat.push({ id, page_id: "", parent_id: parent, type: b.type, props: b.props, position: String(n).padStart(6, "0"), version: 0, created_by: null, updated_by: null, created_at: "", updated_at: "" });
      walk(b.children, id);
    }
  };
  walk(blocks, null);
  return docsBlocksToMarkdown(flat);
}

/** Label and actions of a button: insert blocks below it (written as Markdown), open a link, add a database row. */
export function ButtonEditor({ anchor, orgId, initial, onSave, onClose }: { anchor: HTMLElement; orgId: string; initial: DocsBlock; onSave: (label: string, actions: DocsBlockButtonAction[]) => void; onClose: () => void }) {
  const [label, setLabel] = useState(initial.props.label ?? "");
  const [drafts, setDrafts] = useState<Draft[]>(() =>
    (initial.props.actions ?? []).map((a): Draft => (a.type === "insert" ? { kind: "insert", markdown: asMarkdown(a.blocks) } : a.type === "link" ? { kind: "link", url: a.url } : { kind: "add_row", database_id: a.database_id, title: a.title ?? "", values: a.values })),
  );
  const [error, setError] = useState<string | null>(null);
  const [dbQuery, setDbQuery] = useState("");
  const dbs = useDocsSearch(orgId, dbQuery, { kind: "database", limit: 6 });
  const dbTitle = useMemo(() => new Map((dbs.data ?? []).map((d) => [d.id, docsTitle(d.title)])), [dbs.data]);
  const update = (i: number, d: Draft) => setDrafts((list) => list.map((x, j) => (j === i ? d : x)));
  const save = () => {
    const out: DocsBlockButtonAction[] = [];
    for (const d of drafts) {
      if (d.kind === "insert") {
        const blocks = buttonBlocksFromMarkdown(d.markdown);
        if (blocks.length) out.push({ type: "insert", blocks });
      } else if (d.kind === "link") {
        const href = docsSafeLink(d.url);
        if (!href) return setError("Enter a web address or an email for the link.");
        out.push({ type: "link", url: href });
      } else if (d.database_id) {
        out.push({ type: "add_row", database_id: d.database_id, ...(d.title.trim() ? { title: d.title.trim() } : {}), values: d.values ?? {} });
      } else return setError("Choose the database to add rows to.");
    }
    onSave(label.trim() || "Button", out);
  };
  return (
    <Floating open anchor={anchor} onClose={onClose} autoFocus label="Edit button" className="w-[min(28rem,calc(100vw-2rem))] p-3">
      <form
        className="flex max-h-[70dvh] flex-col gap-3 overflow-y-auto"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Button label" aria-label="Button label" maxLength={100} data-autofocus />
        {drafts.map((d, i) => (
          <div key={i} className="flex flex-col gap-2 rounded-md border border-border p-2">
            <div className="flex items-center justify-between gap-2">
              <span className="flex items-center gap-1 type-label text-fg">
                {d.kind === "insert" ? <ListPlus aria-hidden className="size-4" /> : d.kind === "link" ? <Link2 aria-hidden className="size-4" /> : <Database aria-hidden className="size-4" />}
                {d.kind === "insert" ? "Insert blocks below the button" : d.kind === "link" ? "Open a link" : "Add a row to a database"}
              </span>
              <button type="button" aria-label="Remove this action" className="flex size-8 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring" onClick={() => setDrafts((l) => l.filter((_, j) => j !== i))}>
                <Trash2 aria-hidden className="size-4" />
              </button>
            </div>
            {d.kind === "insert" ? (
              <TextArea value={d.markdown} onChange={(e) => update(i, { ...d, markdown: e.target.value })} rows={4} aria-label="Blocks to insert, in Markdown" placeholder={"- [ ] New task\n## Notes"} className="font-mono" />
            ) : d.kind === "link" ? (
              <Input value={d.url} onChange={(e) => update(i, { ...d, url: e.target.value })} placeholder="https://" aria-label="Link" />
            ) : (
              <div className="flex flex-col gap-2">
                <Input value={dbQuery} onChange={(e) => setDbQuery(e.target.value)} placeholder="Search databases" aria-label="Search databases" />
                <div className="flex flex-col">
                  {(dbs.data ?? []).map((db) => (
                    <button
                      key={db.id}
                      type="button"
                      aria-pressed={d.database_id === db.id}
                      className={cn("flex min-h-control items-center gap-2 rounded-sm px-2 text-left type-body hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", d.database_id === db.id && "bg-primary-soft text-primary-strong")}
                      onClick={() => update(i, { ...d, database_id: db.id })}
                    >
                      <span aria-hidden>{db.icon ?? "🗃️"}</span>
                      <span className="truncate">{docsTitle(db.title)}</span>
                    </button>
                  ))}
                </div>
                {d.database_id && !dbTitle.has(d.database_id) ? <p className="type-caption text-fg-muted">A database is chosen. Search to change it.</p> : null}
                <Input value={d.title} onChange={(e) => update(i, { ...d, title: e.target.value })} placeholder="Title of the new row (optional)" aria-label="Title of the new row" maxLength={300} />
              </div>
            )}
          </div>
        ))}
        {drafts.length < 5 ? (
          <div className="flex flex-wrap gap-2">
            <Button type="button" size="sm" variant="secondary" leftIcon={<ListPlus />} onClick={() => setDrafts((l) => [...l, { kind: "insert", markdown: "" }])}>
              Insert blocks
            </Button>
            <Button type="button" size="sm" variant="secondary" leftIcon={<Link2 />} onClick={() => setDrafts((l) => [...l, { kind: "link", url: "" }])}>
              Open link
            </Button>
            <Button type="button" size="sm" variant="secondary" leftIcon={<Database />} onClick={() => setDrafts((l) => [...l, { kind: "add_row", database_id: "", title: "", values: {} }])}>
              Add a row
            </Button>
          </div>
        ) : null}
        {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" size="sm">
            Save
          </Button>
        </div>
      </form>
    </Floating>
  );
}
