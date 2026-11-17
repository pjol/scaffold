"use client";

import { crmFilterOps, useCrmPipelines, useCrmProperties, useCrmTags, type CrmFilter, type CrmObjectType, type CrmProperty } from "@scaffoldapp/shared";
import { Button, Skeleton } from "@scaffoldapp/ui-web";
import { Plus } from "lucide-react";
import { useCrm } from "./CrmFrame";
import { FilterRow, filterText } from "./ListParts";

// Conditions on a record type's properties, edited inline: lead scoring rules, workflow
// conditions and report filters use the same rows and the same semantics as list filters.

/** Properties a condition can use for a type (filterable only). */
export function useConditionProps(type: CrmObjectType): { props: CrmProperty[]; loading: boolean } {
  const { orgId } = useCrm();
  const q = useCrmProperties(orgId, type);
  return { props: (q.data ?? []).filter((p) => p.filterable), loading: q.isPending };
}

export function ConditionsEditor({
  type,
  filters,
  onChange,
  noMe,
  emptyText,
}: {
  type: CrmObjectType;
  filters: CrmFilter[];
  onChange: (f: CrmFilter[]) => void;
  /** Conditions that apply to everyone can't say "me". */
  noMe?: boolean;
  emptyText?: string;
}) {
  const { orgId, settings } = useCrm();
  const { props, loading } = useConditionProps(type);
  if (loading) return <Skeleton className="h-16 w-full" />;
  const add = () => {
    const p = props[0];
    if (!p) return;
    onChange([...filters, { key: p.key, op: crmFilterOps(p.type)[0]?.op ?? "eq" }]);
  };
  return (
    <div className="flex flex-col gap-2">
      {filters.length === 0 && emptyText ? <p className="type-label text-fg-muted">{emptyText}</p> : null}
      {filters.map((f, i) => (
        <FilterRow
          key={i}
          orgId={orgId}
          props={props}
          filter={f}
          currency={settings.currency}
          objectType={type}
          noMe={!!noMe}
          onChange={(nf) => onChange(filters.map((x, j) => (j === i ? nf : x)))}
          onRemove={() => onChange(filters.filter((_, j) => j !== i))}
        />
      ))}
      <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={add} className="self-start">
        Add condition
      </Button>
    </div>
  );
}

/** "Lifecycle stage is Customer · Job title contains director". */
export function useConditionText(type: CrmObjectType) {
  const { orgId, settings } = useCrm();
  const { props } = useConditionProps(type);
  const tags = useCrmTags(orgId).data ?? [];
  const pipelines = useCrmPipelines(orgId).data ?? [];
  return (filters: CrmFilter[]) => filters.map((f) => filterText(f, props, { tags, pipelines, currency: settings.currency })).join(" · ");
}
