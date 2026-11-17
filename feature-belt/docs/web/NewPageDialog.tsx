"use client";

import {
  docsTitle,
  errorMessage,
  orgsNoun,
  useDocsCreatePage,
  useDocsSidebar,
  useDocsTemplatePreview,
  useDocsTemplates,
  useDocsUseTemplate,
  type DocsTemplate,
} from "@scaffoldapp/shared";
import { Button, Dialog, EmptyState, ErrorState, InfoTip, Input, Select, Skeleton, SkeletonRow, Tab, TabPanel, Tabs, TabsList, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, FilePlus, LayoutTemplate, Search, Table2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { BlockView } from "./BlockView";
import { CoverArt, ImportPanel } from "./Dialogs";
import { PageIcon } from "./PageIcon";

/** Where a new top-level page goes: private, for everyone, or a teamspace. */
type Place = { kind: "private" } | { kind: "shared" } | { kind: "team"; teamId: string };

function placeKey(p: Place): string {
  return p.kind === "team" ? `team:${p.teamId}` : p.kind;
}

function usePlaces(orgId: string, enabled: boolean) {
  const sidebar = useDocsSidebar(enabled ? orgId : null);
  return useMemo(() => {
    const out: { value: string; label: string; place: Place }[] = [{ value: "private", label: "Private, only you", place: { kind: "private" } }];
    if (sidebar.data?.can_create_shared) out.push({ value: "shared", label: `Everyone in the ${orgsNoun()}`, place: { kind: "shared" } });
    for (const t of sidebar.data?.teamspaces ?? []) out.push({ value: `team:${t.team.id}`, label: `${t.team.name} teamspace`, place: { kind: "team", teamId: t.team.id } });
    return out;
  }, [sidebar.data]);
}

function placeBody(p: Place): { shared?: boolean; team_id?: string } {
  if (p.kind === "shared") return { shared: true };
  if (p.kind === "team") return { team_id: p.teamId };
  return {};
}

/** A template's preview before using it. */
function TemplatePreview({ orgId, template, onBack, onUse, busy }: { orgId: string; template: DocsTemplate; onBack: () => void; onUse: () => void; busy: boolean }) {
  const preview = useDocsTemplatePreview(orgId, template.id);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" leftIcon={<ArrowLeft />} onClick={onBack}>
          Templates
        </Button>
        <span className="flex-1" />
        <Button loading={busy} onClick={onUse}>
          Use this template
        </Button>
      </div>
      <div className="max-h-[26rem] overflow-y-auto rounded-md border border-border">
        {template.cover ? <CoverArt cover={template.cover} className="h-20" /> : null}
        <div className="flex flex-col gap-3 p-4">
          <h3 className="flex items-center gap-2 type-title text-fg">
            <PageIcon icon={template.icon} custom={template.custom_icon} kind={template.kind} size="md" />
            {template.name}
            {template.info || template.created_by ? <InfoTip content={template.info ?? `Made by ${template.created_by!.name}`} /> : null}
          </h3>
          {preview.isPending ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-2/3" />
            </div>
          ) : preview.isError ? (
            <ErrorState title="Couldn't load the preview." onRetry={() => void preview.refetch()} />
          ) : (
            <>
              {preview.data.views.length ? <p className="type-label text-fg-muted">Views: {preview.data.views.join(" · ")}</p> : null}
              <BlockView blocks={preview.data.blocks} refs={preview.data.refs} />
              {preview.data.subpages.length ? (
                <div className="flex flex-col gap-1 border-t border-border pt-3">
                  <p className="type-label font-semibold text-fg-muted">Pages inside</p>
                  {preview.data.subpages.map((s) => (
                    <p key={s} className="type-body text-fg">
                      {s}
                    </p>
                  ))}
                </div>
              ) : null}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/** The gallery: built-in templates by category and the workspace's own. */
export function TemplateGallery({ orgId, onPick }: { orgId: string; onPick: (t: DocsTemplate) => void }) {
  const templates = useDocsTemplates(orgId);
  const [q, setQ] = useState("");
  const [cat, setCat] = useState("All");
  if (templates.isPending) {
    return (
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="h-16" />
        ))}
      </div>
    );
  }
  if (templates.isError) return <ErrorState title="Couldn't load templates." onRetry={() => void templates.refetch()} />;
  const all = [...templates.data.workspace, ...templates.data.builtin];
  const cats = ["All", ...new Set(all.map((t) => t.category))];
  const needle = q.trim().toLowerCase();
  const list = all.filter((t) => (cat === "All" || t.category === cat) && (!needle || t.name.toLowerCase().includes(needle) || (t.info ?? "").toLowerCase().includes(needle)));
  return (
    <div className="flex flex-col gap-3">
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search templates" aria-label="Search templates" leadingIcon={<Search />} />
      <div className="flex flex-wrap gap-1" role="tablist" aria-label="Categories">
        {cats.map((c) => (
          <button
            key={c}
            type="button"
            role="tab"
            aria-selected={cat === c}
            onClick={() => setCat(c)}
            className={cn("rounded-pill px-3 py-1 type-label focus-visible:outline-2 focus-visible:outline-ring", cat === c ? "bg-fg text-fg-inverse" : "bg-sunken text-fg-muted hover:text-fg")}
          >
            {c}
          </button>
        ))}
      </div>
      {list.length === 0 ? (
        <EmptyState icon={<LayoutTemplate />} title="No templates match" />
      ) : (
        <ul className="grid max-h-[40dvh] grid-cols-1 gap-2 overflow-y-auto sm:max-h-[22rem] sm:grid-cols-2">
          {list.map((t) => (
            <li key={t.id}>
              <button
                type="button"
                onClick={() => onPick(t)}
                title={t.info}
                className="flex w-full items-center gap-3 rounded-md border border-border p-3 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
              >
                <PageIcon icon={t.icon} custom={t.custom_icon} kind={t.kind} size="md" />
                <span className="flex min-w-0 items-center gap-1 self-center truncate type-label font-semibold text-fg">
                  {t.name}
                  {t.kind === "database" ? <Table2 aria-label="Database" className="size-3.5 shrink-0 text-fg-muted" /> : null}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * New page: empty, from a template, or imported. Inside `parentId`, or at the top level in a place the
 * person picks (private, everyone, or one of their teamspaces).
 */
export function NewPageDialog({
  open,
  onOpenChange,
  orgId,
  parentId = null,
  defaultTab = "empty",
  defaultPlace,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  parentId?: string | null;
  defaultTab?: "empty" | "templates" | "import";
  defaultPlace?: Place;
}) {
  const router = useRouter();
  const create = useDocsCreatePage(orgId);
  const use = useDocsUseTemplate(orgId);
  const places = usePlaces(orgId, open && !parentId);
  const [place, setPlace] = useState<string>(defaultPlace ? placeKey(defaultPlace) : "private");
  const [title, setTitle] = useState("");
  const [picked, setPicked] = useState<DocsTemplate | null>(null);
  const target = places.find((p) => p.value === place)?.place ?? { kind: "private" as const };
  const where = parentId ? { parent_id: parentId } : placeBody(target);
  const done = (id: string) => {
    onOpenChange(false);
    setPicked(null);
    setTitle("");
    router.push(`/docs/${id}`);
  };
  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o ? null : setPicked(null))} title="New page" size="lg">
      <div className="flex flex-col gap-4">
        {!parentId && places.length > 1 ? (
          <div className="flex flex-wrap items-center gap-2">
            <span className="type-label text-fg-muted">In</span>
            <div className="min-w-56">
              <Select value={place} onValueChange={setPlace} aria-label="Where the page goes" options={places.map((p) => ({ value: p.value, label: p.label }))} />
            </div>
          </div>
        ) : null}
        <Tabs defaultValue={defaultTab}>
          <TabsList aria-label="Start from">
            <Tab value="empty">Empty</Tab>
            <Tab value="templates">Templates</Tab>
            <Tab value="import">Import</Tab>
          </TabsList>
          <TabPanel value="empty">
            <form
              className="flex flex-col gap-3 pt-3"
              onSubmit={(e) => {
                e.preventDefault();
                create.mutate({ title: title.trim(), ...where }, { onSuccess: (p) => done(p.id), onError: (err) => toast.error(errorMessage(err, "Couldn't create the page.")) });
              }}
            >
              <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Untitled" aria-label="Title" maxLength={300} autoFocus />
              <div className="flex flex-wrap justify-end gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  leftIcon={<Table2 />}
                  loading={create.isPending && create.variables?.kind === "database"}
                  onClick={() =>
                    create.mutate(
                      { title: title.trim() || "Untitled database", kind: "database", ...where },
                      { onSuccess: (p) => done(p.id), onError: (err) => toast.error(errorMessage(err, "Couldn't create the database.")) },
                    )
                  }
                >
                  Database
                </Button>
                <Button type="submit" leftIcon={<FilePlus />} loading={create.isPending && create.variables?.kind !== "database"}>
                  Page
                </Button>
              </div>
            </form>
          </TabPanel>
          <TabPanel value="templates">
            <div className="pt-3">
              {picked ? (
                <TemplatePreview
                  orgId={orgId}
                  template={picked}
                  busy={use.isPending}
                  onBack={() => setPicked(null)}
                  onUse={() =>
                    use.mutate(
                      { templateId: picked.id, ...where },
                      { onSuccess: (p) => done(p.id), onError: (err) => toast.error(errorMessage(err, `Couldn't make a page from “${docsTitle(picked.name)}”.`)) },
                    )
                  }
                />
              ) : (
                <TemplateGallery orgId={orgId} onPick={setPicked} />
              )}
            </div>
          </TabPanel>
          <TabPanel value="import">
            <div className="pt-3">{open ? <ImportPanel orgId={orgId} parentId={parentId} shared={target.kind === "shared"} onDone={() => onOpenChange(false)} /> : <SkeletonRow />}</div>
          </TabPanel>
        </Tabs>
      </div>
    </Dialog>
  );
}
