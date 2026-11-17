"use client";

import { docsIsComputed, errorMessage, useDocsPressButton, useDocsPutSchema, useDocsSetRowValues, type DocsDatabaseInfo, type DocsPage, type DocsProperty, type DocsRefs } from "@scaffoldapp/shared";
import { Button, ConfirmDialog, IconButton, InfoTip, Spinner, toast } from "@scaffoldapp/ui-web";
import { MousePointerClick, Pencil, Plus } from "lucide-react";
import { useState } from "react";
import { NewPropertyForm, PropertyMenu } from "./database/panels";
import { PropertySheet } from "./database/propertySettings";
import { hasLinks, isEmptyValue, PropertyDisplay, PropertyEditor } from "./database/PropertyValue";
import { Floating } from "./Floating";
import { newShortId, PROPERTY_ICON } from "./format";

/** A database row's property values above its content. */
export function RowProperties({ row, database, refs, editable }: { row: DocsPage; database: DocsDatabaseInfo; refs: DocsRefs; editable: boolean }) {
  const setValues = useDocsSetRowValues(database.id);
  const putSchema = useDocsPutSchema(database.id);
  const press = useDocsPressButton(database.id);
  const [editing, setEditing] = useState<{ id: string; anchor: HTMLElement } | null>(null);
  const [menu, setMenu] = useState<{ id: string; anchor: HTMLElement } | null>(null);
  const [adding, setAdding] = useState<HTMLElement | null>(null);
  const [sheet, setSheet] = useState<{ prop: DocsProperty; isNew: boolean } | null>(null);
  const [deleting, setDeleting] = useState<DocsProperty | null>(null);
  const props = database.schema.filter((p) => p.type !== "title");
  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const editableValue = (p: DocsProperty) => editable && !p.read_only && !docsIsComputed(p);
  const menuProp = menu ? database.schema.find((p) => p.id === menu.id) : undefined;
  const editProp = editing ? database.schema.find((p) => p.id === editing.id) : undefined;
  return (
    <dl className="flex flex-col gap-0.5">
      {props.map((p) => {
        const Icon = PROPERTY_ICON[p.type];
        const value = row.props[p.id];
        const can = editableValue(p);
        return (
          <div key={p.id} className="grid grid-cols-[minmax(7rem,10rem)_1fr] items-start gap-2">
            <dt className="flex min-w-0 items-center">
              <button
                type="button"
                disabled={!editable}
                onClick={(e) => setMenu({ id: p.id, anchor: e.currentTarget })}
                className="flex min-h-control min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-left type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
              >
                <Icon aria-hidden className="size-4 shrink-0" />
                <span className="truncate">{p.name}</span>
              </button>
              {p.description ? <InfoTip content={p.description} label={`About ${p.name}`} /> : null}
            </dt>
            <dd className="min-w-0">
              {p.type === "button" ? (
                <div className="flex min-h-control items-center px-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    leftIcon={press.isPending && press.variables?.propId === p.id ? <Spinner /> : <MousePointerClick />}
                    disabled={!editable || press.isPending}
                    onClick={() =>
                      press.mutate(
                        { rowId: row.id, propId: p.id },
                        {
                          onSuccess: (res) => {
                            for (const u of res.open) window.open(u, "_blank", "noopener,noreferrer");
                            if (res.created.length) toast.success(res.created.length === 1 ? "Added a row." : `Added ${res.created.length} rows.`);
                          },
                          onError: onErr("The button didn't finish."),
                        },
                      )
                    }
                  >
                    {p.button?.label || p.name}
                  </Button>
                </div>
              ) : p.type === "checkbox" ? (
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={value === true}
                  aria-label={p.name}
                  disabled={!can}
                  className="flex min-h-control items-center rounded-sm px-2 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
                  onClick={() => setValues.mutate({ rowId: row.id, values: { [p.id]: value !== true } }, { onError: onErr("Couldn't save that value.") })}
                >
                  <PropertyDisplay prop={p} value={value} refs={refs} row={row} />
                </button>
              ) : hasLinks(p) && !isEmptyValue(value) ? (
                <div className="group/prop flex min-h-control min-w-0 items-center gap-1 rounded-sm px-2 py-1 hover:bg-sunken">
                  <div className="min-w-0 flex-1">
                    <PropertyDisplay prop={p} value={value} refs={refs} row={row} links />
                  </div>
                  {can ? (
                    <span className="shrink-0 sm:opacity-0 sm:group-hover/prop:opacity-100 sm:focus-within:opacity-100">
                      <IconButton label={`Edit ${p.name}`} icon={<Pencil />} size="sm" variant="ghost" onClick={(e) => setEditing({ id: p.id, anchor: e.currentTarget })} />
                    </span>
                  ) : null}
                </div>
              ) : can ? (
                <button
                  type="button"
                  aria-label={`${p.name}, edit`}
                  className="flex min-h-control w-full min-w-0 items-center rounded-sm px-2 py-1 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                  onClick={(e) => setEditing({ id: p.id, anchor: e.currentTarget })}
                >
                  <PropertyDisplay prop={p} value={value} refs={refs} row={row} />
                </button>
              ) : (
                <div className="flex min-h-control min-w-0 items-center px-2 py-1">
                  <PropertyDisplay prop={p} value={value} refs={refs} row={row} links />
                </div>
              )}
            </dd>
          </div>
        );
      })}
      {editable ? (
        <div>
          <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={(e) => setAdding(e.currentTarget)}>
            Add a property
          </Button>
        </div>
      ) : null}
      {editing && editProp ? (
        <Floating open anchor={editing.anchor} onClose={() => setEditing(null)} autoFocus label="Edit value">
          <PropertyEditor
            prop={editProp}
            value={row.props[editing.id]}
            orgId={row.org_id}
            rowId={row.id}
            refs={refs}
            onChange={(v) => setValues.mutate({ rowId: row.id, values: { [editing.id]: v } }, { onError: onErr("Couldn't save that value.") })}
            onSchema={(next) => putSchema.mutate(database.schema.map((x) => (x.id === next.id ? next : x)), { onError: onErr("Couldn't change the options.") })}
            onDone={() => setEditing(null)}
          />
        </Floating>
      ) : null}
      {menu && menuProp ? (
        <Floating open anchor={menu.anchor} onClose={() => setMenu(null)} autoFocus label="Property">
          <PropertyMenu
            prop={menuProp}
            onChange={(next) => putSchema.mutate(database.schema.map((x) => (x.id === next.id ? next : x)), { onError: onErr("Couldn't change the property.") })}
            onEdit={() => {
              setMenu(null);
              setSheet({ prop: menuProp, isNew: false });
            }}
            onDuplicate={
              menuProp.type !== "relation"
                ? () => {
                    setMenu(null);
                    const copy = { ...structuredClone(menuProp), id: newShortId(), name: `${menuProp.name} (copy)` };
                    putSchema.mutate([...database.schema, copy], { onError: onErr("Couldn't duplicate the property.") });
                  }
                : undefined
            }
            onDelete={() => {
              setMenu(null);
              setDeleting(menuProp);
            }}
          />
        </Floating>
      ) : null}
      <Floating open={!!adding} anchor={adding} onClose={() => setAdding(null)} autoFocus label="New property">
        <NewPropertyForm
          onCreate={(p) => {
            setAdding(null);
            putSchema.mutate([...database.schema, p], { onError: onErr("Couldn't add the property.") });
          }}
          onSetup={(p) => {
            setAdding(null);
            setSheet({ prop: p, isNew: true });
          }}
        />
      </Floating>
      {sheet ? <PropertySheet open onOpenChange={(o) => !o && setSheet(null)} dbId={database.id} orgId={row.org_id} schema={database.schema} prop={sheet.prop} isNew={sheet.isNew} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete property"
        tone="danger"
        onConfirm={() => {
          const p = deleting;
          setDeleting(null);
          if (p) putSchema.mutate(database.schema.filter((x) => x.id !== p.id), { onError: onErr("Couldn't delete the property.") });
        }}
      >
        Every row loses its {deleting?.name} value. It can't be undone.
      </ConfirmDialog>
    </dl>
  );
}
