"use client";

import { errorMessage, useDocsPutSchema, useDocsSetRowValues, type DocsDatabaseInfo, type DocsPage, type DocsRefs } from "@scaffoldapp/shared";
import { Button, toast } from "@scaffoldapp/ui-web";
import { Plus } from "lucide-react";
import { useState } from "react";
import { NewPropertyForm, PropertyMenu } from "./database/panels";
import { PropertyDisplay, PropertyEditor } from "./database/PropertyValue";
import { Floating } from "./Floating";
import { PROPERTY_ICON } from "./format";

/** A database row's property values above its content. */
export function RowProperties({ row, database, refs, editable }: { row: DocsPage; database: DocsDatabaseInfo; refs: DocsRefs; editable: boolean }) {
  const setValues = useDocsSetRowValues(database.id);
  const putSchema = useDocsPutSchema(database.id);
  const [editing, setEditing] = useState<{ id: string; anchor: HTMLElement } | null>(null);
  const [menu, setMenu] = useState<{ id: string; anchor: HTMLElement } | null>(null);
  const [adding, setAdding] = useState<HTMLElement | null>(null);
  const props = database.schema.filter((p) => p.type !== "title");
  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  return (
    <dl className="flex flex-col gap-0.5">
      {props.map((p) => {
        const Icon = PROPERTY_ICON[p.type];
        const value = row.props[p.id];
        return (
          <div key={p.id} className="grid grid-cols-[minmax(7rem,10rem)_1fr] items-start gap-2">
            <dt>
              <button
                type="button"
                disabled={!editable}
                onClick={(e) => setMenu({ id: p.id, anchor: e.currentTarget })}
                className="flex min-h-control w-full items-center gap-2 rounded-sm px-2 text-left type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
              >
                <Icon aria-hidden className="size-4 shrink-0" />
                <span className="truncate">{p.name}</span>
              </button>
            </dt>
            <dd>
              {p.type === "checkbox" ? (
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={value === true}
                  aria-label={p.name}
                  disabled={!editable}
                  className="flex min-h-control items-center rounded-sm px-2 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                  onClick={() => setValues.mutate({ rowId: row.id, values: { [p.id]: value !== true } }, { onError: onErr("Couldn't save that value.") })}
                >
                  <PropertyDisplay prop={p} value={value} refs={refs} />
                </button>
              ) : (
                <button
                  type="button"
                  disabled={!editable}
                  aria-label={`${p.name}, edit`}
                  className="flex min-h-control w-full min-w-0 items-center rounded-sm px-2 py-1 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent"
                  onClick={(e) => setEditing({ id: p.id, anchor: e.currentTarget })}
                >
                  <PropertyDisplay prop={p} value={value} refs={refs} />
                </button>
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
      {editing ? (
        <Floating open anchor={editing.anchor} onClose={() => setEditing(null)} autoFocus label="Edit value">
          <PropertyEditor
            prop={database.schema.find((p) => p.id === editing.id)!}
            value={row.props[editing.id]}
            orgId={row.org_id}
            onChange={(v) => setValues.mutate({ rowId: row.id, values: { [editing.id]: v } }, { onError: onErr("Couldn't save that value.") })}
            onSchema={(next) => putSchema.mutate(database.schema.map((x) => (x.id === next.id ? next : x)), { onError: onErr("Couldn't add the option.") })}
            onDone={() => setEditing(null)}
          />
        </Floating>
      ) : null}
      {menu ? (
        <Floating open anchor={menu.anchor} onClose={() => setMenu(null)} autoFocus label="Property">
          <PropertyMenu
            prop={database.schema.find((p) => p.id === menu.id)!}
            onChange={(next) => putSchema.mutate(database.schema.map((x) => (x.id === next.id ? next : x)), { onError: onErr("Couldn't change the property.") })}
            onDelete={() => {
              setMenu(null);
              putSchema.mutate(database.schema.filter((x) => x.id !== menu.id), { onError: onErr("Couldn't delete the property.") });
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
        />
      </Floating>
    </dl>
  );
}
