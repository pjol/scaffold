"use client";

import {
  DOCS_PROPERTY_LABEL,
  docsDatabaseEndpoints,
  docsIsComputed,
  docsKeys,
  docsTitle,
  errorMessage,
  uploadFile,
  useApi,
  useDocsImportCSV,
  useDocsImportRun,
  useDocsPreviewCSV,
  useEndpoints,
  type DocsCSVMapping,
  type DocsCSVPreview,
  type DocsCSVSource,
  type DocsPage,
  type DocsPropertyType,
  type DocsView,
} from "@scaffoldapp/shared";
import { Button, Dialog, Select, Spinner, cn, toast } from "@scaffoldapp/ui-web";
import { useQueryClient } from "@tanstack/react-query";
import { CircleCheck, FileSpreadsheet, TriangleAlert, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";

/** Files up to this size are read in the browser; larger ones upload first. */
const INLINE_MAX = 900 * 1024;

const NEW_TYPES: DocsPropertyType[] = ["text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone", "files"];

function fileName(title: string | null | undefined, suffix = ""): string {
  const base = (docsTitle(title) || "Database").replace(/[\\/:*?"<>|]+/g, " ").trim().slice(0, 80) || "Database";
  return `${base}${suffix}.csv`;
}

/** Download a view's rows (or everything) as CSV. */
export function CsvExportDialog({ db, view, onClose }: { db: DocsPage; view: DocsView; onClose: () => void }) {
  const d = useEndpoints(docsDatabaseEndpoints);
  const [what, setWhat] = useState<"view" | "all">("view");
  const [busy, setBusy] = useState(false);
  const download = async () => {
    setBusy(true);
    try {
      const text = await d.exportCSV(db.id, what === "all" ? { all: true } : { view: view.id });
      const blob = new Blob([text], { type: "text/csv;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = fileName(db.title, what === "view" ? ` - ${view.name}` : "");
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't export the CSV."));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="Export CSV"
      info="Values export as you see them, computed ones included; rows and related rows you can't open are left out."
      footer={
        <Button onClick={() => void download()} loading={busy}>
          Download
        </Button>
      }
    >
      <div role="radiogroup" aria-label="What to export" className="flex flex-col gap-2">
        {(
          [
            ["view", `${view.name}: its filters, sorts and properties`],
            ["all", "Every row and property"],
          ] as const
        ).map(([v, label]) => (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={what === v}
            onClick={() => setWhat(v)}
            className={cn("flex min-h-control items-center gap-3 rounded-md border px-3 py-2 text-left type-label focus-visible:outline-2 focus-visible:outline-ring", what === v ? "border-primary bg-primary-soft text-fg" : "border-border text-fg hover:bg-sunken")}
          >
            <span aria-hidden className={cn("flex size-4 shrink-0 items-center justify-center rounded-pill border", what === v ? "border-primary" : "border-border-strong")}>
              {what === v ? <span className="size-2 rounded-pill bg-primary" /> : null}
            </span>
            {label}
          </button>
        ))}
      </div>
    </Dialog>
  );
}

type Choice = { kind: "skip" } | { kind: "property"; id: string } | { kind: "new"; type: DocsPropertyType };

/** Import a CSV into this database: map its columns, then follow the import. */
export function CsvImportDialog({ db, onClose }: { db: DocsPage; onClose: () => void }) {
  const api = useApi();
  const qc = useQueryClient();
  const preview = useDocsPreviewCSV(db.id);
  const start = useDocsImportCSV(db.id);
  const input = useRef<HTMLInputElement>(null);
  const [src, setSrc] = useState<DocsCSVSource | null>(null);
  const [name, setName] = useState("");
  const [reading, setReading] = useState(false);
  const [plan, setPlan] = useState<DocsCSVPreview | null>(null);
  const [title, setTitle] = useState(0);
  const [choices, setChoices] = useState<Choice[]>([]);
  const [runId, setRunId] = useState<string | null>(null);
  const run = useDocsImportRun(runId);
  const schema = (db.schema ?? []).filter((p) => p.type !== "title" && !docsIsComputed(p) && !p.read_only);
  const finished = run.data && run.data.status !== "running";
  useEffect(() => {
    if (!finished) return;
    void qc.invalidateQueries({ queryKey: docsKeys.rowsAll(db.id) });
    void qc.invalidateQueries({ queryKey: docsKeys.page(db.id) });
  }, [finished, qc, db.id]);

  const pick = async (file: File) => {
    setReading(true);
    setName(file.name);
    try {
      const source: DocsCSVSource = file.size <= INLINE_MAX ? { text: await file.text() } : { key: (await uploadFile(api.http, { file, purpose: "docs_import", contentType: "text/csv", timeoutMs: 600_000 })).key };
      const p = await preview.mutateAsync(source);
      setSrc(source);
      setPlan(p);
      setTitle(p.title_column);
      setChoices(p.columns.map((c) => (c.match && schema.some((x) => x.id === c.match) ? { kind: "property", id: c.match } : { kind: "new", type: NEW_TYPES.includes(c.type) ? c.type : "text" })));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't read that file."));
    } finally {
      setReading(false);
    }
  };
  const submit = () => {
    if (!src || !plan) return;
    const mapping: DocsCSVMapping[] = [];
    plan.columns.forEach((c, i) => {
      if (i === title) return;
      const ch = choices[i];
      if (!ch || ch.kind === "skip") return;
      mapping.push(ch.kind === "property" ? { column: c.index, property: ch.id } : { column: c.index, new: { name: c.name || `Column ${i + 1}`, type: ch.type } });
    });
    start.mutate({ ...src, title_column: plan.columns[title]?.index ?? 0, mapping }, { onSuccess: (r) => setRunId(r.id), onError: (e) => toast.error(errorMessage(e, "Couldn't start the import.")) });
  };

  let body;
  let footer;
  if (runId) {
    const r = run.data;
    body = (
      <div className="flex flex-col gap-3" aria-live="polite">
        {!r || r.status === "running" ? (
          <p className="flex items-center gap-2 type-body text-fg">
            <Spinner />
            {r && r.pages_total ? `Added ${r.pages_done} of ${r.pages_total} rows…` : "Importing…"}
          </p>
        ) : r.status === "done" ? (
          <p className="flex items-center gap-2 type-body text-fg">
            <CircleCheck aria-hidden className="size-5 text-success" />
            Added {r.pages_done} {r.pages_done === 1 ? "row" : "rows"}.
          </p>
        ) : (
          <p className="flex items-center gap-2 type-body text-danger-strong">
            <TriangleAlert aria-hidden className="size-5" />
            {r.message ?? "The import stopped."}
          </p>
        )}
        {r?.errors.length ? (
          <div className="flex flex-col gap-1">
            <p className="type-label font-semibold text-fg">{r.errors.length === 1 ? "1 value wasn't read" : `${r.errors.length} values weren't read`}</p>
            <ul className="flex max-h-48 flex-col gap-0.5 overflow-y-auto rounded-md border border-border p-2">
              {r.errors.map((e, i) => (
                <li key={i} className="type-caption text-fg">
                  <span className="text-fg-muted">{e.path}: </span>
                  {e.message}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    );
    footer = (
      <Button onClick={onClose} disabled={!finished}>
        Done
      </Button>
    );
  } else if (plan) {
    body = (
      <div className="flex flex-col gap-4">
        <p className="type-label text-fg">
          {name}: {plan.rows} {plan.rows === 1 ? "row" : "rows"}, {plan.columns.length} columns
        </p>
        <label className="flex flex-col gap-1 type-label text-fg-muted">
          Title column
          <Select value={String(title)} options={plan.columns.map((c, i) => ({ value: String(i), label: c.name || `Column ${i + 1}` }))} onValueChange={(v) => setTitle(Number(v))} />
        </label>
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[32rem] border-collapse">
            <thead>
              <tr className="border-b border-border">
                <th scope="col" className="px-3 py-2 text-left type-caption font-semibold text-fg-muted">
                  Column
                </th>
                <th scope="col" className="px-3 py-2 text-left type-caption font-semibold text-fg-muted">
                  Goes to
                </th>
              </tr>
            </thead>
            <tbody>
              {plan.columns.map((c, i) => {
                const ch = choices[i] ?? { kind: "skip" };
                const value = i === title ? "title" : ch.kind === "skip" ? "skip" : ch.kind === "property" ? `p:${ch.id}` : "new";
                return (
                  <tr key={c.index} className="border-b border-border last:border-b-0 align-top">
                    <th scope="row" className="max-w-56 px-3 py-2 text-left font-normal">
                      <p className="truncate type-label font-medium text-fg">{c.name || `Column ${i + 1}`}</p>
                      <p className="truncate type-caption text-fg-muted">{c.sample.filter(Boolean).slice(0, 3).join(" · ")}</p>
                    </th>
                    <td className="px-3 py-2">
                      {i === title ? (
                        <p className="flex min-h-control items-center type-label text-fg-muted">Title</p>
                      ) : (
                        <div className="flex flex-col gap-2 sm:flex-row">
                          <Select
                            aria-label={`Where ${c.name || `column ${i + 1}`} goes`}
                            className="min-w-40 flex-1"
                            value={value}
                            options={[
                              { value: "new", label: "New property" },
                              ...schema.map((p) => ({ value: `p:${p.id}`, label: p.name })),
                              { value: "skip", label: "Skip" },
                            ]}
                            onValueChange={(v) =>
                              setChoices((cs) => cs.map((x, j) => (j !== i ? x : v === "skip" ? { kind: "skip" } : v === "new" ? { kind: "new", type: NEW_TYPES.includes(c.type) ? c.type : "text" } : { kind: "property", id: v.slice(2) })))
                            }
                          />
                          {ch.kind === "new" ? (
                            <Select
                              aria-label="Type"
                              className="w-40"
                              value={ch.type}
                              options={NEW_TYPES.map((t) => ({ value: t, label: DOCS_PROPERTY_LABEL[t] }))}
                              onValueChange={(t) => setChoices((cs) => cs.map((x, j) => (j === i ? { kind: "new", type: t as DocsPropertyType } : x)))}
                            />
                          ) : null}
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    );
    footer = (
      <>
        <Button
          variant="ghost"
          onClick={() => {
            setPlan(null);
            setSrc(null);
          }}
        >
          Choose another file
        </Button>
        <Button onClick={submit} loading={start.isPending}>
          Import {plan.rows} {plan.rows === 1 ? "row" : "rows"}
        </Button>
      </>
    );
  } else {
    body = (
      <div className="flex flex-col items-center gap-3 rounded-md border border-dashed border-border px-4 py-8 text-center">
        <FileSpreadsheet aria-hidden className="size-8 text-fg-muted" />
        <input
          ref={input}
          type="file"
          accept=".csv,.tsv,text/csv,text/tab-separated-values"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = "";
            if (f) void pick(f);
          }}
        />
        <Button leftIcon={reading ? <Spinner /> : <Upload />} disabled={reading} onClick={() => input.current?.click()}>
          {reading ? "Reading…" : "Choose a CSV file"}
        </Button>
      </div>
    );
    footer = null;
  }
  return (
    <Dialog open onOpenChange={(o) => !o && (!runId || finished) && onClose()} title={`Import into ${docsTitle(db.title)}`} info="Rows are added; nothing already here changes. Options are matched by name and new ones added; people match by name or email." size="lg" footer={footer}>
      {body}
    </Dialog>
  );
}
