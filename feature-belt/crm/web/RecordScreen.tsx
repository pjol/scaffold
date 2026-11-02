"use client";

import {
  CRM_KIND,
  CRM_TYPE_LABEL,
  accessLevelAtLeast,
  connectionFixPath,
  crmIsConnectedId,
  crmSourceIdOf,
  isConnectionErrorCode,
  crmDuration,
  crmMoney,
  errorMessage,
  useCrmCompanyLinks,
  useCrmDeleteRecord,
  useCrmLink,
  useCrmLinkCompany,
  useCrmMerge,
  useCrmMoveStage,
  useCrmPipelines,
  useCrmRecord,
  useCrmStageHistory,
  useCrmUpdateRecord,
  useTopic,
  topic,
  type CrmAssociations,
  type CrmCompanyLink,
  type CrmDeal,
  type CrmObjectType,
  type CrmRecord,
  type CrmRecordRef,
  type CrmSearchHit,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  IconButton,
  ProviderIcon,
  Select,
  Skeleton,
  SkeletonGroup,
  SourceBadge,
  Tab,
  Tabs,
  TabsList,
  cn,
  sourcePlatformName,
  toast,
} from "@scaffoldapp/ui-web";
import { Copy, ExternalLink, GitMerge, Import, Link2, Mail, Phone, Plus, SearchX, Star, Trash2, Unlink, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { APP_NAME } from "@/lib/app";
import { formatDateTime } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { CrmFrame, useCrm } from "./CrmFrame";
import { CreateRecordDialog } from "./CreateRecordDialog";
import { PropertiesPanel } from "./Properties";
import { TimelinePanel } from "./Timeline";
import { ImportDialog } from "./ListScreen";
import { MoneyText, PersonChip, RecordPicker, StageBadge, TYPE_ICON, TagList, dayLabel, recordHref } from "./parts";
import { openInLabel } from "./Sources";

/** /crm/{companies|contacts|deals}/[id]: properties left, timeline centre, associations right (tabs on narrow screens). */
export function CrmRecordScreen({ type, id }: { type: CrmObjectType; id: string }) {
  return (
    <CrmFrame>
      <RecordView type={type} id={id} />
    </CrmFrame>
  );
}

type Pane = "activity" | "about" | "related";

function RecordView({ type, id }: { type: CrmObjectType; id: string }) {
  const rec = useCrmRecord(type, id);
  const connected = crmIsConnectedId(id);
  const pathname = usePathname() ?? "/crm";
  // Connected records live on their platform: no app topic to follow.
  useTopic(connected ? null : topic("crm", id));
  const [pane, setPane] = useUrlState<Pane>("pane", "activity");

  if (rec.isPending) {
    return (
      <SkeletonGroup label={`Loading the ${CRM_TYPE_LABEL[type].one}`} className="flex flex-col gap-6">
        <Skeleton className="h-24 w-full" />
        <div className="grid gap-6 lg:grid-cols-12">
          <Skeleton className="h-96 lg:col-span-4 xl:col-span-3" />
          <Skeleton className="h-96 lg:col-span-8 xl:col-span-6" />
          <Skeleton className="hidden h-96 xl:col-span-3 xl:block" />
        </div>
      </SkeletonGroup>
    );
  }
  if (rec.isError) {
    if (isConnectionErrorCode(rec.error.code)) {
      return (
        <Card padding="compact">
          <EmptyState
            icon={<SearchX />}
            title={`Couldn't open this ${CRM_TYPE_LABEL[type].one}`}
            info={rec.error.message}
            action={
              rec.error.code === "connection_needs_auth" || rec.error.code === "connection_scope_missing" ? (
                <Button asChild size="auto">
                  <Link href={connectionFixPath(null, pathname)}>Reconnect</Link>
                </Button>
              ) : undefined
            }
          />
        </Card>
      );
    }
    if (rec.error.status === 404) {
      return (
        <Card padding="compact">
          <EmptyState
            icon={<SearchX />}
            title={`${CRM_TYPE_LABEL[type].One} not found`}
            info={connected ? "It was deleted on its platform, or your account there can't see it." : "It was deleted, merged into another record, or isn't shared with you."}
            action={
              <Button asChild size="auto">
                <Link href={`/crm/${CRM_TYPE_LABEL[type].many}`}>{CRM_TYPE_LABEL[type].Many}</Link>
              </Button>
            }
          />
        </Card>
      );
    }
    return <ErrorState title={`Couldn't load this ${CRM_TYPE_LABEL[type].one}`} info={rec.error.message} onRetry={() => void rec.refetch()} retrying={rec.isRefetching} />;
  }
  const { record, associations, can_delete } = rec.data;
  return (
    <div className="flex flex-col gap-6">
      <RecordHeader record={record} canDelete={can_delete} associations={associations} />
      <Tabs value={pane} onValueChange={(v) => setPane(v as Pane)} variant="pill" className="lg:hidden">
        <TabsList aria-label="Record sections">
          <Tab value="activity">Activity</Tab>
          <Tab value="about">About</Tab>
          <Tab value="related">Related</Tab>
        </TabsList>
      </Tabs>
      <div className="grid items-start gap-6 lg:grid-cols-12">
        <aside className={cn("flex flex-col gap-6 lg:col-span-4 xl:col-span-3", pane === "about" ? "flex" : "hidden lg:flex")} aria-label="About">
          <PropertiesPanel record={record} />
          {record.type === "deal" && !record.source ? <StageHistoryCard deal={record} /> : null}
        </aside>
        <section className={cn("flex min-w-0 flex-col gap-4 lg:col-span-8 lg:row-span-2 xl:col-span-6", pane === "activity" ? "flex" : "hidden lg:flex")} aria-label="Activity">
          <TimelinePanel record={record} suggestions={suggestionsFor(record, associations)} />
        </section>
        <aside className={cn("flex flex-col gap-6 lg:col-span-4 xl:col-span-3", pane === "related" ? "flex" : "hidden lg:flex")} aria-label="Related records">
          <AssociationsPanel record={record} associations={associations} />
          {record.type === "company" && !record.source ? <LinkedRecordsCard companyId={record.id} editable={accessLevelAtLeast(record.my_level === "none" ? null : record.my_level, "edit")} /> : null}
        </aside>
      </div>
    </div>
  );
}

/** Records a new activity is also logged on by default: the contact's primary company, the deal's company. */
function suggestionsFor(record: CrmRecord, a: CrmAssociations): CrmRecordRef[] {
  if (record.type === "contact") return a.companies.filter((c) => c.is_primary).map((c) => ({ type: "company", id: c.id, name: c.name }));
  if (record.type === "deal" && record.company) return [record.company];
  return [];
}

function RecordHeader({ record, canDelete, associations }: { record: CrmRecord; canDelete: boolean; associations: CrmAssociations }) {
  const { orgId, settings, sources } = useCrm();
  const router = useRouter();
  const [merging, setMerging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [importing, setImporting] = useState(false);
  const external = record.source?.type === "connected" ? record.source : null;
  const openIn = openInLabel(record.source);
  const canImport = !!external && settings.can.import && sources?.mode !== "connected";
  const del = useCrmDeleteRecord(record.type);
  const Icon = TYPE_ICON[record.type];
  const editable = accessLevelAtLeast(record.my_level === "none" ? null : record.my_level, "edit");
  const email = record.type === "contact" ? record.emails[0] : undefined;
  const phone = record.type === "contact" ? record.phones[0] : record.type === "company" ? record.phone || undefined : undefined;
  return (
    <Card padding="compact" className="flex flex-col gap-4">
      <div className="flex items-start gap-4">
        <span className="hidden size-14 shrink-0 items-center justify-center rounded-lg bg-primary-soft text-primary-strong sm:flex" aria-hidden>
          <Icon className="size-6" />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <h2 className="type-headline break-words text-fg">{record.title}</h2>
            <SourceBadge source={record.source} showAccount size="md" />
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 type-label text-fg-muted">
            {record.type === "contact" ? (
              <>
                {record.job_title ? <span>{record.job_title}</span> : null}
                {record.company ? (
                  <Link href={recordHref("company", record.company.id)} className="font-semibold text-fg hover:underline">
                    {record.company.name}
                  </Link>
                ) : null}
              </>
            ) : record.type === "company" ? (
              <>
                {record.domain ? (
                  <a href={`https://${record.domain}`} target="_blank" rel="noreferrer noopener" className="font-semibold text-fg hover:underline">
                    {record.domain}
                  </a>
                ) : null}
                {record.industry ? <span>{record.industry}</span> : null}
              </>
            ) : (
              <>
                <MoneyText minor={record.amount} currency={record.currency} className="type-title-sm font-semibold text-fg" />
                <StageBadge name={record.stage.name} outcome={record.stage.outcome} />
                {record.close_date ? <span>Closes {dayLabel(record.close_date)}</span> : null}
                {record.company ? (
                  <Link href={recordHref("company", record.company.id)} className="font-semibold text-fg hover:underline">
                    {record.company.name}
                  </Link>
                ) : null}
              </>
            )}
            <TagList tags={record.tags} />
          </div>
        </div>
        <ResourceMenu
          feature="crm"
          // Connected records aren't the app's to share: their platform decides who sees them.
          {...(external ? {} : { kind: CRM_KIND[record.type], id: record.id })}
          title={record.title}
          orgId={orgId}
          actions={[
            ...(openIn && external?.url
              ? [{ id: "crm-open-in", label: openIn, icon: ExternalLink, onSelect: () => void window.open(external.url, "_blank", "noopener,noreferrer") }]
              : []),
            ...(canImport ? [{ id: "crm-import-one", label: `Import into ${APP_NAME}`, icon: Import, onSelect: () => setImporting(true) }] : []),
            {
              id: "crm-copy-link",
              label: "Copy link",
              icon: Copy,
              onSelect: () => {
                void navigator.clipboard?.writeText(`${window.location.origin}${recordHref(record.type, record.id)}`).then(() => toast.success("Link copied"));
              },
            },
            ...(canDelete ? [{ id: "crm-merge", label: "Merge…", icon: GitMerge, onSelect: () => setMerging(true) }] : []),
            ...(canDelete ? [{ id: "crm-delete", label: `Delete ${CRM_TYPE_LABEL[record.type].one}`, icon: Trash2, destructive: true, onSelect: () => setDeleting(true) }] : []),
          ]}
        />
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <span className="flex items-center gap-2 type-label text-fg-muted">
          Owner <PersonChip user={record.owner} />
        </span>
        {record.team ? <Badge>{record.team.name}</Badge> : null}
        <div className="flex flex-wrap gap-2 sm:ml-auto">
          {email ? (
            <Button asChild variant="secondary" size="sm" leftIcon={<Mail />}>
              <a href={`mailto:${email}`}>Email</a>
            </Button>
          ) : null}
          {phone ? (
            <Button asChild variant="secondary" size="sm" leftIcon={<Phone />}>
              <a href={`tel:${phone.replace(/[^\d+]/g, "")}`}>Call</a>
            </Button>
          ) : null}
        </div>
      </div>
      {record.type === "deal" ? <StageBar deal={record} editable={editable} /> : null}
      {merging ? <MergeDialog record={record} onClose={() => setMerging(false)} /> : null}
      {importing && external ? (
        <ImportDialog
          type={record.type}
          orgId={orgId}
          source={external.connection_id}
          ids={[record.id]}
          onClose={() => setImporting(false)}
          onDone={(created) => {
            if (created) router.push(recordHref(record.type, created.id));
          }}
        />
      ) : null}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        tone="danger"
        title={`Delete “${record.title}”?`}
        confirmLabel={`Delete ${CRM_TYPE_LABEL[record.type].one}`}
        onConfirm={() =>
          del.mutateAsync({ id: record.id, orgId }).then(
            () => {
              toast.success(`${CRM_TYPE_LABEL[record.type].One} deleted`);
              router.push(`/crm/${CRM_TYPE_LABEL[record.type].many}`);
            },
            (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
          )
        }
      >
        Its timeline and the notes logged only on it go too
        {associations.contacts.length + associations.deals.length + associations.companies.length > 0 ? "; linked records stay" : ""}. It can&apos;t be undone.
      </ConfirmDialog>
    </Card>
  );
}

/** The deal's pipeline as steps; click one to move the deal (edit needed). */
function StageBar({ deal, editable }: { deal: CrmDeal; editable: boolean }) {
  const { orgId } = useCrm();
  const pipelines = useCrmPipelines(orgId, crmSourceIdOf(deal));
  const move = useCrmMoveStage(orgId);
  const update = useCrmUpdateRecord("deal", deal.id);
  const pl = pipelines.data?.find((p) => p.id === deal.pipeline_id);
  if (!pl) return null;
  const at = pl.stages.findIndex((s) => s.id === deal.stage.id);
  const go = (stageId: string, name: string) => {
    if (stageId === deal.stage.id) return;
    move.mutate(
      { deal, stageId },
      {
        onSuccess: () => {
          toast.success(`Moved to ${name}`);
          void update.reset();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't move the deal.")),
      },
    );
  };
  return (
    <ol className="flex gap-1 overflow-x-auto pb-1" aria-label={`${pl.name} stages`}>
      {pl.stages.map((s, i) => {
        const current = s.id === deal.stage.id;
        const passed = deal.stage.outcome === "open" ? i < at : s.outcome === "open" || current;
        return (
          <li key={s.id} className="min-w-24 flex-1">
            <button
              type="button"
              disabled={!editable || move.isPending}
              aria-current={current ? "step" : undefined}
              onClick={() => go(s.id, s.name)}
              title={`${s.name} · ${s.probability}%`}
              className={cn(
                "flex h-control w-full items-center justify-center truncate rounded-sm px-2 type-caption font-semibold transition-colors disabled:cursor-default",
                current
                  ? s.outcome === "won"
                    ? "bg-success text-success-fg"
                    : s.outcome === "lost"
                      ? "bg-danger text-danger-fg"
                      : "bg-primary text-primary-fg"
                  : passed
                    ? "bg-primary-soft text-primary-strong"
                    : "bg-sunken text-fg-muted",
                editable && !current && "hover:bg-primary-soft hover:text-primary-strong",
              )}
            >
              {s.name}
            </button>
          </li>
        );
      })}
    </ol>
  );
}

function StageHistoryCard({ deal }: { deal: CrmDeal }) {
  const h = useCrmStageHistory(deal.id);
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <h2 className="type-title-sm text-fg">Time in stage</h2>
      {h.isPending ? (
        <SkeletonGroup label="Loading stage history" className="flex flex-col gap-2">
          <Skeleton shape="text" className="h-4 w-full" />
          <Skeleton shape="text" className="h-4 w-2/3" />
        </SkeletonGroup>
      ) : h.isError ? (
        <ErrorState compact title="Couldn't load the history" onRetry={() => void h.refetch()} />
      ) : (
        <ul className="flex flex-col gap-2">
          {h.data.totals.map((t) => (
            <li key={t.stage_id} className="flex items-baseline justify-between gap-3 type-label">
              <span className={cn("truncate", t.stage_id === deal.stage.id ? "font-semibold text-fg" : "text-fg-muted")}>{t.stage_name}</span>
              <span className="tabular-nums text-fg">{crmDuration(t.seconds)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

// ---- associations ---------------------------------------------------------------------

function AssociationsPanel({ record, associations: a }: { record: CrmRecord; associations: CrmAssociations }) {
  const { orgId } = useCrm();
  const link = useCrmLink();
  const update = useCrmUpdateRecord(record.type, record.id);
  // A connected record's links change on its platform; here they're read-only.
  const external = !!record.source;
  const editable = !external && accessLevelAtLeast(record.my_level === "none" ? null : record.my_level, "edit");
  const [adding, setAdding] = useState<CrmObjectType | null>(null);
  const [creating, setCreating] = useState<CrmObjectType | null>(null);
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't change the link."));
  const self: CrmSearchHit = { type: record.type, id: record.id, org_id: record.org_id, title: record.title, detail: "", score: 1, updated_at: record.updated_at };
  const exclude = useMemo(() => new Set([...a.companies.map((c) => c.id), ...a.contacts.map((c) => c.id), ...a.deals.map((d) => d.id)]), [a]);

  const onPick = (hit: CrmSearchHit) => {
    setAdding(null);
    if (record.type === "contact" && hit.type === "company") link.mutate({ op: "add_company", contactId: record.id, companyId: hit.id }, { onError: fail });
    else if (record.type === "company" && hit.type === "contact") link.mutate({ op: "add_company", contactId: hit.id, companyId: record.id }, { onError: fail });
    else if (record.type === "deal" && hit.type === "contact") link.mutate({ op: "add_contact", dealId: record.id, contactId: hit.id }, { onError: fail });
    else if (record.type === "contact" && hit.type === "deal") link.mutate({ op: "add_contact", dealId: hit.id, contactId: record.id }, { onError: fail });
    else if (record.type === "deal" && hit.type === "company") update.mutate({ company: hit.id }, { onError: fail });
  };

  const sections: { type: CrmObjectType; title: string; rows: { id: string; name: string; detail: React.ReactNode; primary?: boolean }[]; canAdd: boolean; canCreate: boolean }[] = [];
  if (record.type !== "company") {
    sections.push({
      type: "company",
      title: record.type === "deal" ? "Company" : "Companies",
      rows: a.companies.map((c) => ({ id: c.id, name: c.name, detail: c.domain ?? "", primary: record.type === "contact" && c.is_primary })),
      canAdd: editable && (record.type === "contact" || a.companies.length === 0),
      canCreate: false,
    });
  }
  if (record.type !== "contact") {
    sections.push({
      type: "contact",
      title: "Contacts",
      rows: a.contacts.map((c) => ({ id: c.id, name: c.name, detail: c.job_title || c.email || "" })),
      canAdd: !external && (record.type === "deal" ? editable : true),
      canCreate: !external && record.type === "company",
    });
  }
  if (record.type !== "deal") {
    sections.push({
      type: "deal",
      title: "Deals",
      rows: a.deals.map((d) => ({
        id: d.id,
        name: d.name,
        detail: (
          <span className="flex items-center gap-2">
            {d.amount !== null ? <span className="tabular-nums">{crmMoney(d.amount, d.currency)}</span> : null}
            <span>{d.stage_name}</span>
          </span>
        ),
      })),
      canAdd: !external && record.type === "contact",
      canCreate: !external,
    });
  }

  const unlink = (type: CrmObjectType, otherId: string) => {
    if (record.type === "contact" && type === "company") link.mutate({ op: "remove_company", contactId: record.id, companyId: otherId }, { onError: fail });
    else if (record.type === "company" && type === "contact") link.mutate({ op: "remove_company", contactId: otherId, companyId: record.id }, { onError: fail });
    else if (record.type === "deal" && type === "contact") link.mutate({ op: "remove_contact", dealId: record.id, contactId: otherId }, { onError: fail });
    else if (record.type === "contact" && type === "deal") link.mutate({ op: "remove_contact", dealId: otherId, contactId: record.id }, { onError: fail });
    else if (record.type === "deal" && type === "company") update.mutate({ company: null }, { onError: fail });
  };

  return (
    <>
      {sections.map((s) => {
        const Icon = TYPE_ICON[s.type];
        return (
          <Card key={s.type} padding="compact" className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <h2 className="flex-1 type-title-sm text-fg">
                {s.title} <span className="type-label text-fg-muted">{s.rows.length || ""}</span>
              </h2>
              {s.canCreate ? (
                <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setCreating(s.type)}>
                  New
                </Button>
              ) : null}
              {s.canAdd ? (
                <Button variant="ghost" size="sm" onClick={() => setAdding(s.type)}>
                  Link
                </Button>
              ) : null}
            </div>
            {s.rows.length === 0 ? (
              <p className="type-label text-fg-subtle">{external ? "None your account can see" : "None you can see"}</p>
            ) : (
              <ul className="flex flex-col gap-1">
                {s.rows.map((r) => (
                  <li key={r.id} className="group flex items-center gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-sunken">
                    <Icon className="size-4 shrink-0 text-fg-muted" aria-hidden />
                    <Link href={recordHref(s.type, r.id)} className="flex min-w-0 flex-1 flex-col">
                      <span className="flex items-center gap-1 truncate type-label font-semibold text-fg">
                        <span className="truncate">{r.name}</span>
                        {r.primary ? <Badge tone="primary">Primary</Badge> : null}
                      </span>
                      {r.detail ? <span className="truncate type-caption text-fg-muted">{r.detail}</span> : null}
                    </Link>
                    {editable && record.type === "contact" && s.type === "company" && !r.primary ? (
                      <IconButton label={`Make ${r.name} primary`} size="sm" icon={<Star />} onClick={() => link.mutate({ op: "primary_company", contactId: record.id, companyId: r.id }, { onError: fail })} />
                    ) : null}
                    {!external && (editable || (record.type === "company" && s.type === "contact")) && !(record.type === "company" && s.type === "deal") ? (
                      <IconButton label={`Unlink ${r.name}`} size="sm" icon={<X />} onClick={() => unlink(s.type, r.id)} />
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </Card>
        );
      })}
      <Dialog open={adding !== null} onOpenChange={(o) => !o && setAdding(null)} title={adding ? `Link a ${CRM_TYPE_LABEL[adding].one}` : "Link"} size="md">
        {adding ? <RecordPicker orgId={orgId} types={[adding]} exclude={exclude} label={`Find a ${CRM_TYPE_LABEL[adding].one}`} autoFocus onPick={onPick} /> : null}
      </Dialog>
      {creating ? (
        <CreateRecordDialog
          type={creating}
          open
          onOpenChange={(o) => !o && setCreating(null)}
          initial={record.type === "company" ? { company: self } : record.type === "contact" ? { contact: self } : {}}
        />
      ) : null}
    </>
  );
}

// ---- merge --------------------------------------------------------------------------------

/** Merge another record of the same type into this one (or this one into it). */
function MergeDialog({ record, onClose }: { record: CrmRecord; onClose: () => void }) {
  const { orgId } = useCrm();
  const router = useRouter();
  const [other, setOther] = useState<CrmSearchHit | null>(null);
  const [keep, setKeep] = useState<"this" | "other">("this");
  const merge = useCrmMerge(orgId, record.type);
  const label = CRM_TYPE_LABEL[record.type];
  const run = () => {
    if (!other) return;
    const primaryId = keep === "this" ? record.id : other.id;
    const secondaryId = keep === "this" ? other.id : record.id;
    merge.mutate(
      { primaryId, secondaryId },
      {
        onSuccess: (d) => {
          toast.success(`Merged into “${d.record.title}”`);
          onClose();
          if (d.record.id !== record.id) router.push(recordHref(record.type, d.record.id));
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't merge.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Merge ${label.many}`}
      info="The kept record takes the other's associations, activities and timeline, and fills its empty fields from it. The other record is deleted, with its own sharing."
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="danger" disabled={!other} loading={merge.isPending} onClick={run}>
            Merge
          </Button>
        </>
      }
    >
      {!other ? (
        <RecordPicker orgId={orgId} types={[record.type]} exclude={new Set([record.id])} label={`Find the duplicate ${label.one}`} autoFocus onPick={setOther} />
      ) : (
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 type-label font-semibold text-fg">Which one do you keep?</legend>
          {(
            [
              ["this", record.title],
              ["other", other.title],
            ] as const
          ).map(([v, name]) => (
            <label key={v} className={cn("flex min-h-tap cursor-pointer items-center gap-3 rounded-md border px-3", keep === v ? "border-primary bg-primary-soft" : "border-border")}>
              <input type="radio" name="keep" className="accent-primary" checked={keep === v} onChange={() => setKeep(v)} />
              <span className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{name}</span>
              {v === "this" ? <span className="type-caption text-fg-muted">This {label.one}</span> : null}
            </label>
          ))}
          <Button variant="ghost" size="sm" className="self-start" onClick={() => setOther(null)}>
            Choose another
          </Button>
        </fieldset>
      )}
    </Dialog>
  );
}

// ---- links to connected CRMs -----------------------------------------------------------

/**
 * A company linked to a HubSpot company or Salesforce account: the platform's deals and latest
 * activity, read with the viewer's own connection (each colleague sees what their account can).
 */
function LinkedRecordsCard({ companyId, editable }: { companyId: string; editable: boolean }) {
  const { orgId, sources } = useCrm();
  const pathname = usePathname() ?? "/crm";
  const links = useCrmCompanyLinks(companyId);
  const change = useCrmLinkCompany(companyId);
  const [picking, setPicking] = useState(false);
  const conns = (sources?.sources ?? []).filter((s) => s.type === "connected" && (s.status ?? "active") === "active");
  const [conn, setConn] = useState<string>("");
  const chosen = conn || conns[0]?.id || "";
  if (!sources || sources.mode === "app") return null;
  const items = links.data ?? [];
  if (items.length === 0 && (!editable || conns.length === 0)) return null;
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't change the link."));
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <h2 className="flex-1 type-title-sm text-fg">Linked records</h2>
        {editable && conns.length > 0 ? (
          <Button variant="ghost" size="sm" leftIcon={<Link2 />} onClick={() => setPicking(true)}>
            Link
          </Button>
        ) : null}
      </div>
      {links.isPending ? (
        <SkeletonGroup label="Loading linked records" className="flex flex-col gap-2">
          <Skeleton shape="text" className="h-4 w-40" />
          <Skeleton shape="text" className="h-4 w-56" />
        </SkeletonGroup>
      ) : links.isError ? (
        <ErrorState compact title="Couldn't load linked records" onRetry={() => void links.refetch()} />
      ) : items.length === 0 ? (
        <p className="type-label text-fg-subtle">Link this company to its HubSpot or Salesforce record to see deals and activity from there.</p>
      ) : (
        <ul className="flex flex-col gap-4">
          {items.map((l) => (
            <LinkedRecord key={l.id} link={l} pathname={pathname} onUnlink={() => change.mutate({ op: "unlink", linkId: l.id }, { onError: fail })} />
          ))}
        </ul>
      )}
      <Dialog open={picking} onOpenChange={setPicking} title="Link a connected company" size="md">
        <div className="flex flex-col gap-3">
          {conns.length > 1 ? (
            <Select aria-label="Account" value={chosen} onValueChange={setConn} options={conns.map((c) => ({ value: c.id, label: c.label }))} />
          ) : null}
          {chosen ? (
            <RecordPicker
              orgId={orgId}
              types={["company"]}
              source={chosen}
              label="Find the company"
              autoFocus
              onPick={(hit) => {
                setPicking(false);
                change.mutate({ op: "link", recordId: hit.id }, { onSuccess: () => toast.success(`Linked to ${hit.title}`), onError: fail });
              }}
            />
          ) : null}
        </div>
      </Dialog>
    </Card>
  );
}

function LinkedRecord({ link: l, pathname, onUnlink }: { link: CrmCompanyLink; pathname: string; onUnlink: () => void }) {
  const platform = sourcePlatformName(l.provider);
  return (
    <li className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <ProviderIcon provider={l.provider} size={16} />
        {l.status === "ok" && l.record ? (
          <Link href={recordHref("company", l.record.id)} className="min-w-0 flex-1 truncate type-label font-semibold text-fg hover:underline">
            {l.record.title}
          </Link>
        ) : (
          <span className="min-w-0 flex-1 truncate type-label font-semibold text-fg">{platform}</span>
        )}
        {l.can_remove ? <IconButton label={`Unlink from ${platform}`} size="sm" icon={<Unlink />} onClick={onUnlink} /> : null}
      </div>
      {l.status === "no_connection" ? (
        <Button asChild variant="secondary" size="sm" className="self-start">
          <Link href={connectionFixPath(l.capability, pathname)}>Connect {platform} to see it</Link>
        </Button>
      ) : l.status !== "ok" ? (
        <p className="type-caption text-fg-muted">{l.message}</p>
      ) : (
        <>
          {l.deals.length > 0 ? (
            <ul className="flex flex-col gap-1">
              {l.deals.slice(0, 5).map((d) => (
                <li key={d.id} className="flex items-center gap-2 type-caption">
                  <Link href={recordHref("deal", d.id)} className="min-w-0 flex-1 truncate text-fg hover:underline">
                    {d.name}
                  </Link>
                  {d.amount !== null ? <MoneyText minor={d.amount} currency={d.currency} className="text-fg-muted" /> : null}
                  <StageBadge name={d.stage_name} outcome={d.stage_outcome} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="type-caption text-fg-subtle">No deals there your account can see</p>
          )}
          {l.timeline.length > 0 ? (
            <ul className="flex flex-col gap-1 border-t border-border pt-2">
              {l.timeline.map((a) => (
                <li key={a.id} className="flex items-baseline gap-2 type-caption">
                  <span className="min-w-0 flex-1 truncate text-fg">{a.subject || a.body || a.type}</span>
                  <time className="shrink-0 text-fg-muted" dateTime={a.occurred_at}>
                    {formatDateTime(a.occurred_at)}
                  </time>
                </li>
              ))}
            </ul>
          ) : null}
        </>
      )}
    </li>
  );
}
