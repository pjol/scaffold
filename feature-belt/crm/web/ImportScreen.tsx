"use client";

import {
  CRM_TYPE_LABEL,
  errorMessage,
  useCrmImportRun,
  useCrmImports,
  useCrmInspectImport,
  useCrmPipelines,
  useCrmPreviewImport,
  useCrmRunImport,
  type CrmDedupe,
  type CrmImportInspect,
  type CrmImportPreview,
  type CrmObjectType,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, SegmentedControl, Select, SkeletonGroup, SkeletonRow, Spinner, cn, toast } from "@scaffoldapp/ui-web";
import { FileUp, Lock, Upload } from "lucide-react";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { formatRelative } from "@/lib/format";
import { CrmFrame, useCrm } from "./CrmFrame";

const MAX_BYTES = 5 * 1024 * 1024;

/** /crm/import: upload a CSV, map columns, preview every row, import in the background. */
export function CrmImportScreen() {
  return (
    <CrmFrame>
      <ImportGate />
    </CrmFrame>
  );
}

function ImportGate() {
  const { settings } = useCrm();
  if (!settings.can.import) {
    return (
      <Card padding="compact">
        <EmptyState icon={<Lock />} title="You can't import records here" info="An admin can give you the “Import CRM records” permission in Settings → Permissions." />
      </Card>
    );
  }
  return (
    <div className="flex flex-col gap-section">
      <Wizard />
      <History />
    </div>
  );
}

type Step = "file" | "map" | "preview" | "run";

function Wizard() {
  const { orgId } = useCrm();
  const [type, setType] = useState<CrmObjectType>("contact");
  const [step, setStep] = useState<Step>("file");
  const [file, setFile] = useState<{ name: string; text: string } | null>(null);
  const [inspect, setInspect] = useState<CrmImportInspect | null>(null);
  const [mapping, setMapping] = useState<(string | null)[]>([]);
  const [dedupe, setDedupe] = useState<CrmDedupe>("update");
  const [createCompanies, setCreateCompanies] = useState(true);
  const [pipelineId, setPipelineId] = useState("");
  const [preview, setPreview] = useState<CrmImportPreview | null>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const inspector = useCrmInspectImport(orgId);
  const previewer = useCrmPreviewImport(orgId);
  const runner = useCrmRunImport(orgId);
  const pipelines = useCrmPipelines(type === "deal" ? orgId : null);

  const body = () => ({
    object_type: type,
    filename: file?.name ?? "",
    csv: file?.text ?? "",
    mapping,
    dedupe: type === "deal" ? ("create" as const) : dedupe,
    create_companies: createCompanies,
    ...(type === "deal" && pipelineId ? { pipeline_id: pipelineId } : {}),
  });

  const choose = async (f: File | undefined) => {
    if (!f) return;
    if (f.size > MAX_BYTES) return setError("That file is over 5 MB. Split it into smaller files.");
    setError(null);
    const text = await f.text();
    setFile({ name: f.name, text });
    inspector.mutate(
      { object_type: type, csv: text, filename: f.name },
      {
        onSuccess: (r) => {
          setInspect(r);
          setMapping(r.mapping);
          setStep("map");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't read that file.")),
      },
    );
  };
  const doPreview = () => {
    if (!mapping.some(Boolean)) return setError("Map at least one column.");
    setError(null);
    previewer.mutate(body(), {
      onSuccess: (p) => {
        setPreview(p);
        setStep("preview");
      },
      onError: (e) => setError(errorMessage(e, "Couldn't check the rows.")),
    });
  };
  const doRun = () =>
    runner.mutate(body(), {
      onSuccess: (r) => {
        setRunId(r.id);
        setStep("run");
      },
      onError: (e) => setError(errorMessage(e, "Couldn't start the import.")),
    });
  const restart = () => {
    setStep("file");
    setFile(null);
    setInspect(null);
    setPreview(null);
    setRunId(null);
    setError(null);
    if (inputRef.current) inputRef.current.value = "";
  };

  return (
    <Section title={`Import ${CRM_TYPE_LABEL[type].many}`} info="CSV with a header row, up to 5 MB and 5,000 rows. Contacts match existing ones by email, companies by domain, among the records you can see.">
      <Card padding="compact" className="flex flex-col gap-6">
        <ol className="flex flex-wrap gap-2" aria-label="Steps">
          {(["file", "map", "preview", "run"] as Step[]).map((s, i) => (
            <li key={s}>
              <Badge tone={step === s ? "primary" : "neutral"} variant={step === s ? "solid" : "soft"}>
                {i + 1}. {{ file: "File", map: "Columns", preview: "Check", run: "Import" }[s]}
              </Badge>
            </li>
          ))}
        </ol>
        {error ? <Banner tone="danger">{error}</Banner> : null}
        {step === "file" ? (
          <div className="flex flex-col gap-4">
            <SegmentedControl<CrmObjectType>
              aria-label="What to import"
              className="sm:max-w-md"
              value={type}
              onValueChange={setType}
              options={[
                { value: "contact", label: "Contacts" },
                { value: "company", label: "Companies" },
                { value: "deal", label: "Deals" },
              ]}
            />
            <label
              className={cn(
                "flex min-h-40 cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border-2 border-dashed border-border-strong p-6 text-center transition-colors hover:bg-sunken",
                inspector.isPending && "pointer-events-none opacity-60",
              )}
              onDragOver={(e) => e.preventDefault()}
              onDrop={(e) => {
                e.preventDefault();
                void choose(e.dataTransfer.files[0]);
              }}
            >
              {inspector.isPending ? <Spinner size={24} label="Reading the file" /> : <FileUp className="size-8 text-fg-muted" aria-hidden />}
              <span className="type-label font-semibold text-fg">Choose a CSV file or drop it here</span>
              <input ref={inputRef} type="file" accept=".csv,text/csv" className="sr-only" onChange={(e) => void choose(e.target.files?.[0])} />
            </label>
          </div>
        ) : null}
        {step === "map" && inspect ? (
          <div className="flex flex-col gap-4">
            <p className="type-label text-fg-muted">
              {file?.name} · {inspect.row_count.toLocaleString()} rows
            </p>
            <div className="w-full overflow-x-auto">
              <Table label="Column mapping" className="min-w-160">
                <THead>
                  <TH>Column</TH>
                  <TH>Examples</TH>
                  <TH>Imports into</TH>
                </THead>
                <tbody>
                  {inspect.headers.map((h, i) => (
                    <TR key={i}>
                      <TD className="font-semibold">{h || `Column ${i + 1}`}</TD>
                      <TD className="max-w-64 truncate text-fg-muted">
                        {inspect.sample
                          .map((r) => r[i])
                          .filter(Boolean)
                          .slice(0, 2)
                          .join(" · ")}
                      </TD>
                      <TD>
                        <Select
                          aria-label={`Field for ${h}`}
                          className="w-56"
                          value={mapping[i] ?? "skip"}
                          onValueChange={(v) => setMapping(mapping.map((m, j) => (j === i ? (v === "skip" ? null : v) : m)))}
                          options={[{ value: "skip", label: "Don't import" }, ...inspect.targets.map((t) => ({ value: t.key, label: t.label }))]}
                        />
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              {type !== "deal" ? (
                <Field label="When a record already exists" info={type === "contact" ? "Matched by any email address." : "Matched by domain."}>
                  <Select
                    value={dedupe}
                    onValueChange={(v) => setDedupe(v as CrmDedupe)}
                    options={[
                      { value: "update", label: "Update it" },
                      { value: "skip", label: "Skip the row" },
                      { value: "create", label: "Add a duplicate" },
                    ]}
                  />
                </Field>
              ) : (
                <Field label="Pipeline" info="Rows without a Pipeline column go here; a missing Stage means its first open stage.">
                  <Select value={pipelineId || pipelines.data?.[0]?.id} onValueChange={setPipelineId} options={(pipelines.data ?? []).map((p) => ({ value: p.id, label: p.name }))} />
                </Field>
              )}
              {type !== "company" && mapping.includes("company") ? (
                <Checkbox label="Add companies that don't exist yet" checked={createCompanies} onCheckedChange={setCreateCompanies} className="self-end" />
              ) : null}
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="secondary" onClick={restart}>
                Start over
              </Button>
              <Button loading={previewer.isPending} onClick={doPreview}>
                Check rows
              </Button>
            </div>
          </div>
        ) : null}
        {step === "preview" && preview ? (
          <div className="flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Count label="New" n={preview.create} tone="success" />
              <Count label="Updated" n={preview.update} tone="primary" />
              <Count label="Skipped" n={preview.skip} tone="neutral" />
              <Count label="With errors" n={preview.error} tone="danger" />
            </div>
            {preview.new_tags.length || preview.new_companies ? (
              <p className="type-label text-fg-muted">
                {preview.new_companies ? `${preview.new_companies} new compan${preview.new_companies === 1 ? "y" : "ies"}. ` : ""}
                {preview.new_tags.length ? `New tags: ${preview.new_tags.join(", ")}.` : ""}
              </p>
            ) : null}
            <div className="max-h-96 w-full overflow-auto rounded-md border border-border">
              <Table label="Rows" className="min-w-160">
                <THead>
                  <TH>Line</TH>
                  <TH>Record</TH>
                  <TH>Result</TH>
                </THead>
                <tbody>
                  {preview.rows.map((r) => (
                    <TR key={r.row}>
                      <TD className="tabular-nums text-fg-muted">{r.row}</TD>
                      <TD className="max-w-48 truncate">{r.title || "—"}</TD>
                      <TD>
                        <span className="flex flex-wrap items-center gap-2">
                          <Badge tone={r.action === "create" ? "success" : r.action === "update" ? "primary" : r.action === "error" ? "danger" : "neutral"}>
                            {{ create: "New", update: "Update", skip: "Skip", error: "Error" }[r.action]}
                          </Badge>
                          {r.message ? <span className={cn("type-caption", r.action === "error" ? "text-danger-strong" : "text-fg-muted")}>{r.message}</span> : null}
                        </span>
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button variant="secondary" onClick={() => setStep("map")}>
                Back
              </Button>
              <Button leftIcon={<Upload />} loading={runner.isPending} disabled={preview.create + preview.update === 0} onClick={doRun}>
                Import {(preview.create + preview.update).toLocaleString()} rows
              </Button>
            </div>
          </div>
        ) : null}
        {step === "run" && runId ? <RunStatus id={runId} type={type} onAgain={restart} /> : null}
      </Card>
    </Section>
  );
}

function Count({ label, n, tone }: { label: string; n: number; tone: "success" | "primary" | "neutral" | "danger" }) {
  return (
    <div className="flex flex-col gap-1 rounded-md bg-sunken p-3">
      <span className="type-caption text-fg-muted">{label}</span>
      <span className={cn("type-title tabular-nums", tone === "danger" && n > 0 ? "text-danger-strong" : tone === "success" && n > 0 ? "text-success-strong" : "text-fg")}>{n.toLocaleString()}</span>
    </div>
  );
}

function RunStatus({ id, type, onAgain }: { id: string; type: CrmObjectType; onAgain: () => void }) {
  const { orgId } = useCrm();
  const run = useCrmImportRun(orgId, id);
  const status = run.data?.status;
  useEffect(() => {
    if (status === "done" && run.data) toast.success(`Imported: ${run.data.created_count} new, ${run.data.updated_count} updated`);
  }, [status]); // once, when the run finishes
  if (run.isPending) return <Spinner size={24} label="Starting the import" />;
  if (run.isError) return <ErrorState compact title="Couldn't check the import" onRetry={() => void run.refetch()} />;
  const r = run.data;
  const pct = r.total_rows ? Math.round((r.processed / r.total_rows) * 100) : 0;
  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      {r.status === "running" ? (
        <>
          <p className="type-label text-fg">
            Importing… {r.processed.toLocaleString()} of {r.total_rows.toLocaleString()} rows
          </p>
          <div className="h-2 w-full overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
            <div className="h-full rounded-pill bg-primary transition-[width] duration-(--app-duration-base)" style={{ width: `${pct}%` }} />
          </div>
        </>
      ) : (
        <Banner tone={r.status === "done" ? (r.error_count ? "warning" : "success") : "danger"}>
          {r.status === "done"
            ? `${r.created_count} new, ${r.updated_count} updated, ${r.skipped_count} skipped${r.error_count ? `, ${r.error_count} with errors` : ""}.`
            : "The import stopped. Rows before the error were saved; import the rest again."}
        </Banner>
      )}
      {r.errors.length > 0 ? (
        <ul className="flex max-h-64 flex-col gap-1 overflow-y-auto type-label">
          {r.errors.map((e) => (
            <li key={e.row} className="flex gap-3">
              <span className="w-16 shrink-0 tabular-nums text-fg-muted">Line {e.row}</span>
              <span className="text-danger-strong">{e.message}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {r.status !== "running" ? (
        <div className="flex flex-wrap justify-end gap-2">
          <Button variant="secondary" onClick={onAgain}>
            Import another file
          </Button>
          <Button asChild>
            <Link href={`/crm/${CRM_TYPE_LABEL[type].many}`}>Open {CRM_TYPE_LABEL[type].many}</Link>
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function History() {
  const { orgId } = useCrm();
  const list = useCrmImports(orgId);
  return (
    <Section title="Recent imports">
      <Card padding="none" className="p-1">
        {list.isPending ? (
          <SkeletonGroup label="Loading imports">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load imports" onRetry={() => void list.refetch()} />
        ) : list.data.length === 0 ? (
          <EmptyState compact icon={<Upload />} title="No imports yet" />
        ) : (
          <ul className="flex flex-col">
            {list.data.map((r) => (
              <li key={r.id} className="flex min-h-row flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 type-label">
                <span className="min-w-0 flex-1 truncate font-semibold text-fg">{r.filename || CRM_TYPE_LABEL[r.object_type].Many}</span>
                <Badge tone={r.status === "done" ? (r.error_count ? "warning" : "success") : r.status === "failed" ? "danger" : "info"}>{r.status === "running" ? "Running" : r.status === "done" ? "Done" : "Stopped"}</Badge>
                <span className="text-fg-muted tabular-nums">
                  {r.created_count} new · {r.updated_count} updated{r.error_count ? ` · ${r.error_count} error${r.error_count === 1 ? "" : "s"}` : ""}
                </span>
                <span className="text-fg-subtle">
                  {r.created_by?.name ?? "Former member"} · {formatRelative(r.created_at)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </Section>
  );
}
