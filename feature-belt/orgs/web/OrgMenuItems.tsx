"use client";

import { orgsNounTitle, useActiveOrg } from "@scaffoldapp/shared";
import { MenuItem, MenuLabel, MenuSeparator } from "@scaffoldapp/ui-web";
import { Building2, Check } from "lucide-react";
import Link from "next/link";

/**
 * The org switcher inside the account menu (registry `accountMenuItems` slot): your orgs
 * with the active one checked, then a link to manage them. Nothing while loading.
 */
export function OrgMenuItems() {
  const { org, orgs, isPending, setActive } = useActiveOrg();
  if (isPending) return null;
  return (
    <>
      {orgs.length > 0 && (
        <>
          <MenuLabel>
            <span className="type-caption font-semibold text-fg-muted">{orgsNounTitle()}</span>
          </MenuLabel>
          {orgs.map((o) => (
            <MenuItem key={o.id} icon={o.id === org?.id ? <Check aria-hidden /> : <span className="block size-4" aria-hidden />} onSelect={() => setActive(o.id)}>
              <span className="truncate">{o.name}</span>
            </MenuItem>
          ))}
        </>
      )}
      <MenuItem asChild>
        <Link href={org ? `/settings/orgs/${org.id}` : "/settings/orgs"}>
          <Building2 aria-hidden />
          {orgs.length > 0 ? `${orgsNounTitle()} settings` : orgsNounTitle("many")}
        </Link>
      </MenuItem>
      <MenuSeparator />
    </>
  );
}
