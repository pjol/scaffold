"use client";

import { MoreVertical } from "lucide-react";
import { Fragment } from "react";
import { IconButton, Menu, MenuChoice, MenuItem, MenuSeparator, MenuSub } from "@scaffoldapp/ui-web";
import { resourceMenuProviders, type ResourceMenuAction, type ResourceMenuContext } from "@/features/registry";

export interface ResourceMenuProps extends ResourceMenuContext {
  /** The screen's own actions (Rename, Move, Delete…), shown first; destructive ones go last. */
  actions?: ResourceMenuAction[];
  /** Accessible name for the button. @default "More actions" */
  label?: string;
  align?: "start" | "end";
  className?: string;
}

/**
 * The one ⋮ button every resource and feature screen uses for secondary actions. It shows
 * the screen's own actions, then what installed features add for any resource through
 * `resourceMenuProviders` (Share…, AI agent access…), so those controls sit in the same
 * place everywhere. Pass `kind` and `id` for one resource; only `feature` for a whole section.
 */
export function ResourceMenu({ actions = [], label = "More actions", align = "end", className, ...ctx }: ResourceMenuProps) {
  // The provider list is static, so calling each hook in order keeps hook order stable.
  const provided = resourceMenuProviders.map((p) => ({ id: p.id, ...p.useActions(ctx) }));
  const groups = [actions.filter((a) => !a.destructive), provided.flatMap((p) => p.actions), actions.filter((a) => a.destructive)].filter(
    (g) => g.length > 0,
  );
  return (
    <>
      {groups.length > 0 ? (
        <Menu
          align={align}
          trigger={
            <IconButton label={label} icon={<MoreVertical />} variant="ghost" size="sm" {...(className ? { className } : {})} />
          }
        >
          {groups.map((group, gi) => (
            <Fragment key={gi}>
              {gi > 0 ? <MenuSeparator /> : null}
              {group.map((a) => (
                <ResourceMenuRow key={a.id} action={a} />
              ))}
            </Fragment>
          ))}
        </Menu>
      ) : null}
      {provided.map((p) => (p.element ? <Fragment key={p.id}>{p.element}</Fragment> : null))}
    </>
  );
}

function ResourceMenuRow({ action: a }: { action: ResourceMenuAction }) {
  const Icon = a.icon;
  if (a.choices) {
    return (
      <MenuSub icon={<Icon />} label={a.label} hint={a.hint} disabled={a.disabled}>
        {a.choices.map((c) => (
          <MenuChoice key={c.id} checked={c.checked} hint={c.hint} disabled={c.disabled} onSelect={c.onSelect}>
            {c.label}
          </MenuChoice>
        ))}
      </MenuSub>
    );
  }
  return (
    <MenuItem icon={<Icon />} destructive={a.destructive} disabled={a.disabled} {...(a.onSelect ? { onSelect: a.onSelect } : {})}>
      {a.hint ? (
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate">{a.label}</span>
          <span className="ml-auto shrink-0 type-caption text-fg-muted">{a.hint}</span>
        </span>
      ) : (
        a.label
      )}
    </MenuItem>
  );
}
