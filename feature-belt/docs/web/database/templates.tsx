"use client";

import {
  docsTimeZone,
  docsTitle,
  errorMessage,
  useDocsCreateRowTemplate,
  useDocsRowTemplates,
  useDocsSetTemplateRepeat,
  useDocsTrashPage,
  type DocsRepeat,
  type DocsRowTemplate,
  type DocsView,
  type DocsViewConfig,
} from "@scaffoldapp/shared";
import { Button, Dialog, Field, IconButton, Input, Select, Spinner, cn, toast } from "@scaffoldapp/ui-web";
import { Check, ChevronDown, Ellipsis, FileText, Pencil, Plus, Repeat, Star, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Floating, floatingItemClass } from "../Floating";
import { timeZones } from "./PropertyValue";

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];

function repeatText(r: DocsRepeat): string {
  const every = r.interval > 1 ? `Every ${r.interval} ${r.every}s` : r.every === "day" ? "Every day" : r.every === "week" ? "Every week" : "Every month";
  const on = r.every === "week" && r.weekdays?.length ? ` on ${r.weekdays.map((d) => WEEKDAYS[d]).join(", ")}` : r.every === "month" && r.day ? ` on day ${r.day}` : "";
  return `${every}${on} at ${r.time}`;
}

/** "New" and the templates menu beside it: new rows from a template, the view's default, repeats. */
export function NewWithTemplates({ dbId, orgId, view, config, canEditView, creating, onCreate, onConfig }: {
  dbId: string;
  orgId: string;
  view: DocsView;
  config: DocsViewConfig;
  canEditView: boolean;
  creating: boolean;
  onCreate: (templateId?: string | null) => void;
  onConfig: (cfg: DocsViewConfig) => void;
}) {
  const router = useRouter();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [menu, setMenu] = useState<{ t: DocsRowTemplate; anchor: HTMLElement } | null>(null);
  const [repeat, setRepeat] = useState<DocsRowTemplate | null>(null);
  const templates = useDocsRowTemplates(anchor ? dbId : null);
  const create = useDocsCreateRowTemplate(dbId);
  const trash = useDocsTrashPage(orgId);
  const def = config.template_id ?? null;
  const setDefault = (id: string | null) => {
    const next = { ...config };
    if (id) next.template_id = id;
    else delete next.template_id;
    onConfig(next);
  };
  const close = () => {
    setAnchor(null);
    setMenu(null);
  };
  return (
    <div className="flex items-center">
      <Button size="sm" leftIcon={<Plus />} loading={creating} onClick={() => onCreate()} className="rounded-r-none">
        New
      </Button>
      <IconButton label="New from a template" icon={<ChevronDown />} size="sm" variant="primary" className="rounded-l-none border-l border-primary-strong" onClick={(e) => setAnchor(e.currentTarget)} />
      <Floating open={!!anchor} anchor={anchor} onClose={close} autoFocus placement="bottom-end" label="Templates">
        <div className="flex w-72 flex-col">
          <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">Templates for {view.name}</p>
          <div role="menu" className="flex max-h-80 flex-col overflow-y-auto">
            <button
              type="button"
              role="menuitem"
              className={floatingItemClass}
              onClick={() => {
                close();
                onCreate(null);
              }}
            >
              <FileText aria-hidden />
              <span className="min-w-0 flex-1 truncate">Empty row</span>
              {!def ? <Check aria-label="Default" className="text-primary-strong" /> : null}
            </button>
            {templates.isPending ? (
              <p className="flex items-center gap-2 px-3 py-2 type-label text-fg-muted">
                <Spinner /> Loading templates…
              </p>
            ) : null}
            {templates.isError ? <p className="px-3 py-2 type-label text-danger-strong">Couldn't load the templates.</p> : null}
            {(templates.data?.items ?? []).map((t) => (
              <div key={t.id} className="flex items-center">
                <button
                  type="button"
                  role="menuitem"
                  className={cn(floatingItemClass, "flex-1")}
                  onClick={() => {
                    close();
                    onCreate(t.id);
                  }}
                >
                  {t.icon ? <span aria-hidden>{t.icon}</span> : <FileText aria-hidden />}
                  <span className="min-w-0 flex-1 truncate">{docsTitle(t.title)}</span>
                  {t.repeat ? <Repeat aria-label={repeatText(t.repeat)} className="text-fg-muted" /> : null}
                  {def === t.id ? <Check aria-label="Default" className="text-primary-strong" /> : null}
                </button>
                {canEditView ? <IconButton label={`More for ${docsTitle(t.title)}`} icon={<Ellipsis />} size="sm" variant="ghost" onClick={(e) => setMenu({ t, anchor: e.currentTarget })} /> : null}
              </div>
            ))}
          </div>
          {canEditView ? (
            <button
              type="button"
              className={cn(floatingItemClass, "border-t border-border")}
              disabled={create.isPending}
              onClick={() =>
                create.mutate(
                  { title: "" },
                  {
                    onSuccess: (p) => {
                      close();
                      router.push(`/docs/${p.id}`);
                    },
                    onError: (e) => toast.error(errorMessage(e, "Couldn't add the template.")),
                  },
                )
              }
            >
              {create.isPending ? <Spinner /> : <Plus aria-hidden />}
              New template
            </button>
          ) : null}
        </div>
      </Floating>
      <Floating open={!!menu} anchor={menu?.anchor ?? null} onClose={() => setMenu(null)} autoFocus placement="right-start" label="Template actions">
        {menu ? (
          <div role="menu" className="flex w-60 flex-col">
            <button type="button" role="menuitem" className={floatingItemClass} onClick={() => (close(), router.push(`/docs/${menu.t.id}`))}>
              <Pencil aria-hidden />
              Edit template
            </button>
            <button type="button" role="menuitem" className={floatingItemClass} onClick={() => (setDefault(def === menu.t.id ? null : menu.t.id), setMenu(null))}>
              <Star aria-hidden />
              {def === menu.t.id ? "Stop using as default" : `Default for ${view.name}`}
            </button>
            <button type="button" role="menuitem" className={floatingItemClass} onClick={() => (setRepeat(menu.t), close())}>
              <Repeat aria-hidden />
              {menu.t.repeat ? "Change repeat" : "Repeat…"}
            </button>
            <button
              type="button"
              role="menuitem"
              className={cn(floatingItemClass, "text-danger-strong")}
              onClick={() => {
                const t = menu.t;
                close();
                trash.mutate({ pageId: t.id, databaseId: dbId }, { onSuccess: () => toast.success("Template moved to the trash."), onError: (e) => toast.error(errorMessage(e, "Couldn't delete the template.")) });
                if (def === t.id) setDefault(null);
              }}
            >
              <Trash2 aria-hidden />
              Delete template
            </button>
          </div>
        ) : null}
      </Floating>
      {repeat ? <RepeatDialog dbId={dbId} template={repeat} onClose={() => setRepeat(null)} /> : null}
    </div>
  );
}

/** Make a template add a row on a schedule. */
function RepeatDialog({ dbId, template, onClose }: { dbId: string; template: DocsRowTemplate; onClose: () => void }) {
  const set = useDocsSetTemplateRepeat(dbId);
  const r = template.repeat;
  const [every, setEvery] = useState<DocsRepeat["every"]>(r?.every ?? "week");
  const [interval, setInterval] = useState(String(r?.interval ?? 1));
  const [weekdays, setWeekdays] = useState<number[]>(r?.weekdays ?? [0]);
  const [day, setDay] = useState(String(r?.day ?? 1));
  const [time, setTime] = useState(r?.time ?? "09:00");
  const [tz, setTz] = useState(r?.tz ?? docsTimeZone());
  const save = (repeat: DocsRepeat | null) =>
    set.mutate(
      { templateId: template.id, repeat },
      {
        onSuccess: () => {
          toast.success(repeat ? "The template repeats." : "The template stopped repeating.");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save the repeat.")),
      },
    );
  const n = Math.max(1, Math.min(99, Number(interval) || 1));
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`Repeat “${docsTitle(template.title)}”`}
      info="A new row from this template is added on each date, as you."
      footer={
        <>
          {r ? (
            <Button variant="ghost" onClick={() => save(null)} disabled={set.isPending}>
              Stop repeating
            </Button>
          ) : null}
          <Button
            loading={set.isPending}
            disabled={every === "week" && weekdays.length === 0}
            onClick={() => save({ every, interval: n, time, tz, ...(every === "week" ? { weekdays: [...weekdays].sort() } : {}), ...(every === "month" ? { day: Math.max(1, Math.min(31, Number(day) || 1)) } : {}) })}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex gap-3">
          <Field label="Every" className="w-24">
            <Input inputMode="numeric" value={interval} onChange={(e) => setInterval(e.target.value.replace(/\D/g, "").slice(0, 2))} />
          </Field>
          <Field label="Unit" className="flex-1">
            <Select
              value={every}
              options={[
                { value: "day", label: n > 1 ? "Days" : "Day" },
                { value: "week", label: n > 1 ? "Weeks" : "Week" },
                { value: "month", label: n > 1 ? "Months" : "Month" },
              ]}
              onValueChange={(v) => setEvery(v as DocsRepeat["every"])}
            />
          </Field>
        </div>
        {every === "week" ? (
          <div role="group" aria-label="On" className="flex flex-wrap gap-1">
            {WEEKDAYS.map((d, i) => (
              <button
                key={d}
                type="button"
                aria-pressed={weekdays.includes(i)}
                onClick={() => setWeekdays((w) => (w.includes(i) ? w.filter((x) => x !== i) : [...w, i]))}
                className={cn("h-control min-w-11 rounded-md border px-2 type-label focus-visible:outline-2 focus-visible:outline-ring", weekdays.includes(i) ? "border-primary bg-primary-soft text-fg" : "border-border text-fg-muted hover:bg-sunken")}
              >
                {d}
              </button>
            ))}
          </div>
        ) : null}
        {every === "month" ? (
          <Field label="Day of the month">
            <Input inputMode="numeric" value={day} onChange={(e) => setDay(e.target.value.replace(/\D/g, "").slice(0, 2))} />
          </Field>
        ) : null}
        <div className="flex flex-col gap-3 sm:flex-row">
          <Field label="Time" className="sm:w-36">
            <Input type="time" value={time} onChange={(e) => setTime(e.target.value)} />
          </Field>
          <Field label="Time zone" className="flex-1">
            <Select value={tz} options={timeZones().map((z) => ({ value: z, label: z.replace(/_/g, " ") }))} onValueChange={setTz} />
          </Field>
        </div>
        {template.repeat_next_at ? <p className="type-label text-fg-muted">Next: {new Date(template.repeat_next_at).toLocaleString()}</p> : null}
      </div>
    </Dialog>
  );
}
