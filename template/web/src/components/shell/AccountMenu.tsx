"use client";

import type { Me } from "@scaffoldapp/shared";
import { Menu, MenuItem, MenuLabel, MenuSeparator, motion, useMotionTokens } from "@scaffoldapp/ui-web";
import { LogOut, Settings, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { useSignOut } from "@/components/auth/useSignOut";
import { accountMenuItems, type NavItem } from "@/features/registry";
import { AppAvatar } from "./AppAvatar";

/** Avatar button → menu: overflow sections, the registry's `accountMenuItems`, Settings, Admin (staff), Sign out. */
export function AccountMenu({ me, overflow = [] }: { me: Me; overflow?: readonly NavItem[] }) {
  const t = useMotionTokens();
  const signOut = useSignOut();
  const name = me.display_name || me.email;
  return (
    <Menu
      trigger={
        <motion.button
          type="button"
          whileTap={{ scale: 0.94 }}
          transition={t.spring.press}
          aria-label="Account menu"
          className="relative flex size-control items-center justify-center rounded-pill before:absolute before:inset-0 before:content-[''] pointer-coarse:before:-inset-0.5"
        >
          <AppAvatar name={name} src={me.avatar_url} size="sm" eager />
        </motion.button>
      }
    >
      <MenuLabel>
        <AppAvatar name={name} src={me.avatar_url} size="sm" />
        <span className="min-w-0 truncate type-body font-semibold text-fg">{name}</span>
      </MenuLabel>
      <MenuSeparator />
      {overflow.map((item) => {
        const Icon = item.icon;
        return (
          <MenuItem key={item.href} asChild>
            <Link href={item.href}>
              <Icon aria-hidden />
              {item.label}
            </Link>
          </MenuItem>
        );
      })}
      {accountMenuItems.map((item) =>
        "Component" in item ? (
          <item.Component key={item.id} />
        ) : (
          <MenuItem key={item.id} asChild>
            <Link href={item.href}>
              <item.icon aria-hidden />
              {item.label}
            </Link>
          </MenuItem>
        ),
      )}
      <MenuItem asChild>
        <Link href="/settings">
          <Settings aria-hidden />
          Settings
        </Link>
      </MenuItem>
      {me.admin_role && (
        <MenuItem asChild>
          <Link href="/admin">
            <ShieldCheck aria-hidden />
            Admin
          </Link>
        </MenuItem>
      )}
      <MenuSeparator />
      <MenuItem icon={<LogOut aria-hidden />} onSelect={() => void signOut()}>
        Sign out
      </MenuItem>
    </Menu>
  );
}
