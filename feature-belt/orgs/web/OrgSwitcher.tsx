"use client";

import { useActiveOrg } from "@scaffoldapp/shared";
import { Button, Menu, MenuItem, MenuLabel, MenuSeparator } from "@scaffoldapp/ui-web";
import { Building2, Check, ChevronsUpDown, Plus, Settings2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { CreateOrgDialog } from "./CreateOrgDialog";

/**
 * Pick the active organization (stored in this browser), create one, or open their
 * settings. Drop it into the top bar or account menu; features read the choice with
 * `useActiveOrg()`. Renders a "New organization" button when you have none.
 */
export function OrgSwitcher({ className }: { className?: string }) {
  const { org, orgs, isPending, setActive } = useActiveOrg();
  const [creating, setCreating] = useState(false);
  if (isPending) return null;
  return (
    <>
      {orgs.length === 0 ? (
        <Button variant="secondary" size="auto" leftIcon={<Plus />} className={className} onClick={() => setCreating(true)}>
          New organization
        </Button>
      ) : (
        <Menu
          align="start"
          trigger={
            <Button variant="secondary" size="auto" className={className} rightIcon={<ChevronsUpDown />} aria-label={`Organization: ${org?.name ?? "none"}. Switch`}>
              <span className="flex min-w-0 items-center gap-2">
                <AppAvatar name={org?.name ?? "?"} src={org?.avatar_url ?? null} size="xs" />
                <span className="truncate">{org?.name}</span>
              </span>
            </Button>
          }
        >
          <MenuLabel>
            <Building2 className="size-4 text-fg-muted" aria-hidden />
            <span className="type-label font-semibold text-fg">Organizations</span>
          </MenuLabel>
          <MenuSeparator />
          {orgs.map((o) => (
            <MenuItem key={o.id} icon={o.id === org?.id ? <Check aria-hidden /> : <span className="size-4" aria-hidden />} onSelect={() => setActive(o.id)}>
              <span className="truncate">{o.name}</span>
            </MenuItem>
          ))}
          <MenuSeparator />
          <MenuItem icon={<Plus aria-hidden />} onSelect={() => setCreating(true)}>
            New organization
          </MenuItem>
          <MenuItem asChild>
            <Link href={org ? `/settings/orgs/${org.id}` : "/settings/orgs"}>
              <Settings2 aria-hidden />
              Organization settings
            </Link>
          </MenuItem>
        </Menu>
      )}
      <CreateOrgDialog open={creating} onOpenChange={setCreating} />
    </>
  );
}
