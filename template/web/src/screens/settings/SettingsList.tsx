"use client";

import { Button, Card, ListRow, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { LogOut } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useSignOut } from "@/components/auth/useSignOut";
import { Screen } from "@/components/shell/PageHeader";
import type { SectionLink } from "@/components/shell/SectionNav";
import { useSettingsLinks } from "./links";

function Row({ link }: { link: SectionLink }) {
  const Icon = link.icon;
  return (
    <ListRow
      asChild
      title={link.label}
      leading={
        <span className="flex size-8 items-center justify-center rounded-md bg-sunken text-fg">
          <Icon className="size-4" aria-hidden />
        </span>
      }
    >
      <Link href={link.href} />
    </ListRow>
  );
}

/** Settings on phones: every section as a row, Admin for staff, then Sign out. */
export function SettingsList() {
  const { sections, admin } = useSettingsLinks();
  const signOut = useSignOut();
  const [leaving, setLeaving] = useState(false);
  return (
    <Screen title="Settings">
      <div className="flex flex-col gap-4">
        <Card padding="none" className="p-1">
          <Stagger as="ul" className="flex flex-col">
            {sections.map((link) => (
              <StaggerItem as="li" key={link.href}>
                <Row link={link} />
              </StaggerItem>
            ))}
          </Stagger>
        </Card>
        {admin && (
          <Card padding="none" className="p-1">
            <Row link={admin} />
          </Card>
        )}
      </div>
      <Button
        variant="ghost"
        size="auto"
        leftIcon={<LogOut />}
        loading={leaving}
        className="self-start"
        onClick={() => {
          setLeaving(true);
          void signOut().catch(() => setLeaving(false));
        }}
      >
        Sign out
      </Button>
    </Screen>
  );
}
