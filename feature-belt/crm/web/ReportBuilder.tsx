"use client";

import {
  CRM_OBJECT_TYPES,
  CRM_TYPE_LABEL,
  crmFilterReady,
  useCrmProperties,
  useCrmRunReport,
  type CrmFilter,
  type CrmObjectType,
  type CrmReportBucket,
  type CrmReportChart,
  type CrmReportMeasure,
  type CrmReportSpec,
} from "@scaffoldapp/shared";
import { Banner, Button, Dialog, Field, Input, SegmentedControl, Select, Skeleton } from "@scaffoldapp/ui-web";
import { useEffect, useMemo, useState } from "react";
import { CRM_CHART_LABEL, ReportChart } from "./Charts";
import { ConditionsEditor } from "./Conditions";
import { useCrm } from "./CrmFrame";

const GROUP_TYPES = new Set(["select", "user", "team", "pipeline", "stage", "tags", "checkbox", "date", "datetime", "text", "company", "domain", "currency_code", "multi_select"]);
const BUCKETS: { value: CrmReportBucket; label: string }[] = [
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
  { value: "quarter", label: "Quarter" },
  { value: "year", label: "Year" },
];

/** The measure's label for table headings: "Count", "Sum of Amount". */
export function measureLabel(spec: CrmReportSpec, propLabel?: string): string {
  if (spec.measure === "count") return "Count";
  return `${spec.measure === "sum" ? "Sum" : "Average"} of ${propLabel ?? spec.property ?? ""}`;
}

/** Build or change a report: object, measure, grouping, filters and chart, with a live preview over the records you can see. */
export function ReportBuilderDialog({
  title,
  initial,
  initialName,
  saving,
  onSave,
  onClose,
}: {
  title: string;
  initial?: CrmReportSpec;
  initialName?: string;
  saving: boolean;
  onSave: (name: string, spec: CrmReportSpec) => void;
  onClose: () => void;
}) {
  const { orgId } = useCrm();
  const [name, setName] = useState(initialName ?? "");
  const [object, setObject] = useState<CrmObjectType>(initial?.object ?? "deal");
  const [measure, setMeasure] = useState<CrmReportMeasure>(initial?.measure ?? "count");
  const [property, setProperty] = useState(initial?.property ?? "");
  const [groupBy, setGroupBy] = useState(initial?.group_by ?? "");
  const [bucket, setBucket] = useState<CrmReportBucket>(initial?.bucket ?? "month");
  const [filters, setFilters] = useState<CrmFilter[]>(initial?.filters ?? []);
  const [chart, setChart] = useState<CrmReportChart>(initial?.chart ?? "bar");
  const [error, setError] = useState<string | null>(null);
  const props = useCrmProperties(orgId, object).data ?? [];
  const numeric = props.filter((p) => p.type === "number" || p.type === "currency");
  const groupable = props.filter((p) => p.filterable && GROUP_TYPES.has(p.type));
  const groupProp = groupable.find((p) => p.key === groupBy);
  const isDate = groupProp?.type === "date" || groupProp?.type === "datetime";

  useEffect(() => {
    if (measure !== "count" && !numeric.some((p) => p.key === property)) setProperty(numeric[0]?.key ?? "");
  }, [measure, numeric, property]);

  const spec = useMemo<CrmReportSpec | null>(() => {
    if (measure !== "count" && !property) return null;
    return {
      object,
      measure,
      ...(measure !== "count" ? { property } : {}),
      ...(groupBy ? { group_by: groupBy } : {}),
      ...(groupBy && isDate ? { bucket } : {}),
      filters: filters.filter(crmFilterReady),
      chart,
    };
  }, [object, measure, property, groupBy, isDate, bucket, filters, chart]);
  const preview = useCrmRunReport(orgId, spec);
  const measureProp = numeric.find((p) => p.key === property);

  const save = () => {
    if (!name.trim()) return setError("Name the report.");
    if (!spec) return setError("Choose what to add up.");
    setError(null);
    onSave(name.trim(), spec);
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={title}
      info="Numbers come from the records each viewer can open, so a shared dashboard shows each person their own totals."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={saving} onClick={save}>
            Save report
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name" required error={error}>
          <Input value={name} maxLength={120} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Open pipeline by stage" />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Records">
            <Select
              value={object}
              onValueChange={(v) => {
                setObject(v as CrmObjectType);
                setGroupBy("");
                setFilters([]);
              }}
              options={CRM_OBJECT_TYPES.map((t) => ({ value: t, label: CRM_TYPE_LABEL[t].Many }))}
            />
          </Field>
          <Field label="Measure">
            <Select
              value={measure}
              onValueChange={(v) => setMeasure(v as CrmReportMeasure)}
              options={[
                { value: "count", label: "Count" },
                { value: "sum", label: "Sum" },
                { value: "avg", label: "Average" },
              ]}
            />
          </Field>
        </div>
        {measure !== "count" ? (
          <Field label={measure === "sum" ? "Sum of" : "Average of"}>
            {numeric.length === 0 ? (
              <Banner tone="info">{CRM_TYPE_LABEL[object].Many} have no number or amount properties.</Banner>
            ) : (
              <Select value={property || undefined} onValueChange={setProperty} options={numeric.map((p) => ({ value: p.key, label: p.label }))} />
            )}
          </Field>
        ) : null}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Group by">
            <Select value={groupBy || "none"} onValueChange={(v) => setGroupBy(v === "none" ? "" : v)} options={[{ value: "none", label: "Nothing (one total)" }, ...groupable.map((p) => ({ value: p.key, label: p.label }))]} />
          </Field>
          {isDate ? (
            <Field label="By">
              <Select value={bucket} onValueChange={(v) => setBucket(v as CrmReportBucket)} options={BUCKETS} />
            </Field>
          ) : null}
        </div>
        <Field label="Chart">
          <SegmentedControl<CrmReportChart>
            value={chart}
            onValueChange={setChart}
            options={(["bar", "line", "donut", "table", "number"] as CrmReportChart[]).map((c) => ({ value: c, label: CRM_CHART_LABEL[c] }))}
          />
        </Field>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 type-label font-semibold text-fg">Filters</legend>
          <ConditionsEditor key={object} type={object} filters={filters} onChange={setFilters} emptyText={`Every ${CRM_TYPE_LABEL[object].one} you can see.`} />
        </fieldset>
        <section aria-label="Preview" className="flex flex-col gap-2 rounded-lg border border-border p-4">
          <h3 className="type-label font-semibold text-fg-muted">Preview</h3>
          {preview.isPending && spec ? (
            <Skeleton className="h-40 w-full" />
          ) : preview.isError ? (
            <Banner tone="danger">{preview.error.message}</Banner>
          ) : preview.data ? (
            <ReportChart result={preview.data} chart={chart} name={name || "Preview"} groupLabel={groupProp?.label} measureLabel={measureLabel(preview.data.spec, measureProp?.label)} stale={preview.isFetching} />
          ) : (
            <p className="type-label text-fg-muted">Choose what to add up.</p>
          )}
        </section>
      </div>
    </Dialog>
  );
}
