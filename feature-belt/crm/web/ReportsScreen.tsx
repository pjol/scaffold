"use client";

import {
  CRM_TYPE_LABEL,
  errorMessage,
  useCrmChangeDashboard,
  useCrmDashboards,
  useCrmProperties,
  useCrmReportDashboard,
  type CrmReport,
  type CrmReportDashboard,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, InfoTip, Input, Menu, MenuItem, MenuSeparator, Skeleton, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowUp, BarChart3, ChevronRight, LayoutDashboard, MoreHorizontal, Pencil, Plus, Target, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatRelative } from "@/lib/format";
import { ReportChart } from "./Charts";
import { CrmFrame, useCrm } from "./CrmFrame";
import { ReportBuilderDialog, measureLabel } from "./ReportBuilder";

/** /crm/reports: dashboards you can open (yours and the ones shared with you). */
export function CrmReportsScreen() {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame
      actions={
        <>
          <Button asChild variant="secondary" size="auto" leftIcon={<Target />}>
            <Link href="/crm/forecast">Forecast</Link>
          </Button>
          <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
            New dashboard
          </Button>
        </>
      }
    >
      <Dashboards />
      {creating ? <NameDialog title="New dashboard" onClose={() => setCreating(false)} /> : null}
    </CrmFrame>
  );
}

function Dashboards() {
  const { orgId } = useCrm();
  const list = useCrmDashboards(orgId);
  return (
    <section className="flex flex-col gap-4" aria-label="Dashboards">
      <h2 className="flex items-center gap-2 type-title-sm text-fg">Dashboards</h2>
      {list.isPending ? (
        <Card padding="none" className="p-1">
          <SkeletonGroup label="Loading dashboards">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState title="Couldn't load dashboards" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
      ) : list.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState icon={<LayoutDashboard />} title="No dashboards yet" info="A dashboard holds reports: counts, sums and averages of your records, grouped and charted. Share one and each person sees the totals of the records they can open." />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {list.data.items.map((d) => (
              <li key={d.id}>
                <Link href={`/crm/reports/${d.id}`} className="flex items-center gap-3 px-4 py-3 hover:bg-sunken">
                  <LayoutDashboard className="size-5 shrink-0 text-fg-muted" aria-hidden />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate type-label font-semibold text-fg">{d.name}</span>
                    <span className="truncate type-caption text-fg-muted">
                      {d.report_count === 1 ? "1 report" : `${d.report_count ?? 0} reports`} · {d.owner ? d.owner.name : "No owner"} · changed {formatRelative(d.updated_at)}
                    </span>
                  </span>
                  {d.my_level === "view" || d.my_level === "comment" ? <Badge>View only</Badge> : null}
                  <ChevronRight className="size-4 text-fg-subtle" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </section>
  );
}

function NameDialog({ title, dashboard, onClose }: { title: string; dashboard?: CrmReportDashboard; onClose: () => void }) {
  const { orgId } = useCrm();
  const change = useCrmChangeDashboard(orgId);
  const router = useRouter();
  const [name, setName] = useState(dashboard?.name ?? "");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (!name.trim()) return setError("Name the dashboard.");
    change.mutate(dashboard ? { op: "rename", id: dashboard.id, name: name.trim() } : { op: "create", name: name.trim() }, {
      onSuccess: (d) => {
        onClose();
        if (!dashboard && d && "reports" in d) router.push(`/crm/reports/${d.id}`);
      },
      onError: (e) => setError(errorMessage(e, "Couldn't save the dashboard.")),
    });
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={title}
      size="sm"
      {...(!dashboard ? { info: "Only you can open it until you share it from its ⋮ menu." } : {})}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={change.isPending} onClick={submit}>
            Save
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" required error={error}>
          <Input value={name} maxLength={120} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Sales overview" />
        </Field>
      </form>
    </Dialog>
  );
}

/** /crm/reports/[id]: a dashboard's reports, each computed over the records you can open. */
export function CrmReportDashboardScreen({ id }: { id: string }) {
  return (
    <CrmFrame>
      <DashboardView id={id} />
    </CrmFrame>
  );
}

function DashboardView({ id }: { id: string }) {
  const { orgId } = useCrm();
  const router = useRouter();
  const dash = useCrmReportDashboard(id);
  const change = useCrmChangeDashboard(orgId);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<CrmReport | null>(null);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  if (dash.isPending) {
    return (
      <SkeletonGroup label="Loading the dashboard" className="grid gap-4 md:grid-cols-2">
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>
    );
  }
  if (dash.isError) {
    return dash.error.status === 404 ? (
      <Card padding="compact">
        <EmptyState icon={<LayoutDashboard />} title="Dashboard not found" info="It was deleted, or isn't shared with you." />
      </Card>
    ) : (
      <ErrorState title="Couldn't load the dashboard" info={dash.error.message} onRetry={() => void dash.refetch()} retrying={dash.isRefetching} />
    );
  }
  const d = dash.data;
  const reports = d.reports ?? [];
  const canEdit = d.my_level === "edit" || d.my_level === "manage";
  const reorder = (from: number, to: number) => {
    const a = reports[from];
    const b = reports[to];
    if (!a || !b) return;
    change.mutate({ op: "update_report", id: d.id, reportId: a.id, position: b.position });
    change.mutate({ op: "update_report", id: d.id, reportId: b.id, position: a.position });
  };
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Link href="/crm/reports" className="inline-flex w-fit items-center gap-1 type-label text-fg-muted hover:text-fg hover:underline">
            <ArrowLeft className="size-4" aria-hidden />
            Dashboards
          </Link>
          <h2 className="type-headline break-words text-fg">{d.name}</h2>
        </div>
        {canEdit ? (
          <Button leftIcon={<Plus />} onClick={() => setAdding(true)}>
            Add report
          </Button>
        ) : null}
        <ResourceMenu
          feature="crm"
          kind="crm.dashboard"
          id={d.id}
          title={d.name}
          orgId={orgId}
          actions={[
            ...(canEdit ? [{ id: "crm-dash-rename", label: "Rename", icon: Pencil, onSelect: () => setRenaming(true) }] : []),
            ...(d.my_level === "manage" ? [{ id: "crm-dash-delete", label: "Delete dashboard", icon: Trash2, destructive: true, onSelect: () => setDeleting(true) }] : []),
          ]}
        />
      </div>
      {reports.length === 0 ? (
        <Card padding="compact">
          <EmptyState icon={<BarChart3 />} title="No reports yet" {...(canEdit ? { action: <Button size="auto" onClick={() => setAdding(true)}>Add report</Button> } : {})} />
        </Card>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {reports.map((r, i) => (
            <li key={r.id} className="min-w-0">
              <ReportCard
                report={r}
                canEdit={canEdit}
                onEdit={() => setEditing(r)}
                onUp={i > 0 ? () => reorder(i, i - 1) : undefined}
                onDown={i < reports.length - 1 ? () => reorder(i, i + 1) : undefined}
                onDelete={() => change.mutate({ op: "delete_report", id: d.id, reportId: r.id }, { onSuccess: () => toast.success("Report removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove it.")) })}
              />
            </li>
          ))}
        </ul>
      )}
      {adding ? (
        <ReportBuilderDialog
          title="New report"
          saving={change.isPending}
          onClose={() => setAdding(false)}
          onSave={(name, spec) => change.mutate({ op: "add_report", id: d.id, name, spec }, { onSuccess: () => setAdding(false), onError: (e) => toast.error(errorMessage(e, "Couldn't save the report.")) })}
        />
      ) : null}
      {editing ? (
        <ReportBuilderDialog
          title="Edit report"
          initial={editing.spec}
          initialName={editing.name}
          saving={change.isPending}
          onClose={() => setEditing(null)}
          onSave={(name, spec) => change.mutate({ op: "update_report", id: d.id, reportId: editing.id, name, spec }, { onSuccess: () => setEditing(null), onError: (e) => toast.error(errorMessage(e, "Couldn't save the report.")) })}
        />
      ) : null}
      {renaming ? <NameDialog title="Rename dashboard" dashboard={d} onClose={() => setRenaming(false)} /> : null}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete “${d.name}”?`}
        confirmLabel="Delete dashboard"
        onConfirm={() =>
          change.mutateAsync({ op: "delete", id: d.id }).then(
            () => {
              toast.success("Dashboard deleted");
              router.push("/crm/reports");
            },
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
          )
        }
      >
        Its reports go for everyone it&apos;s shared with. The records stay.
      </ConfirmDialog>
    </div>
  );
}

function ReportCard({ report: r, canEdit, onEdit, onUp, onDown, onDelete }: { report: CrmReport; canEdit: boolean; onEdit: () => void; onUp?: (() => void) | undefined; onDown?: (() => void) | undefined; onDelete: () => void }) {
  const { orgId } = useCrm();
  const props = useCrmProperties(orgId, r.spec.object).data ?? [];
  const groupLabel = props.find((p) => p.key === r.spec.group_by)?.label;
  const propLabel = props.find((p) => p.key === r.spec.property)?.label;
  return (
    <Card padding="compact" className="flex h-full flex-col gap-3">
      <div className="flex items-start gap-2">
        <div className="flex min-w-0 flex-1 items-center gap-1">
          <h3 className="truncate type-title-sm text-fg">{r.name}</h3>
          <InfoTip label={`About ${r.name}`} content={`${measureLabel(r.spec, propLabel)} of ${CRM_TYPE_LABEL[r.spec.object].many}${groupLabel ? ` by ${groupLabel.toLowerCase()}` : ""}, from the records you can open.`} />
        </div>
        {canEdit ? (
          <Menu trigger={<IconButton label={`Actions for ${r.name}`} size="sm" icon={<MoreHorizontal />} />}>
            <MenuItem icon={<Pencil />} onSelect={onEdit}>
              Edit
            </MenuItem>
            {onUp ? (
              <MenuItem icon={<ArrowUp />} onSelect={onUp}>
                Move earlier
              </MenuItem>
            ) : null}
            {onDown ? (
              <MenuItem icon={<ArrowDown />} onSelect={onDown}>
                Move later
              </MenuItem>
            ) : null}
            <MenuSeparator />
            <MenuItem icon={<Trash2 />} destructive onSelect={onDelete}>
              Remove
            </MenuItem>
          </Menu>
        ) : null}
      </div>
      {r.error ? (
        <p className="type-label text-danger-strong">{r.error}</p>
      ) : r.result ? (
        <ReportChart result={r.result} chart={r.spec.chart} name={r.name} groupLabel={groupLabel} measureLabel={measureLabel(r.spec, propLabel)} maxBars={8} />
      ) : null}
    </Card>
  );
}
