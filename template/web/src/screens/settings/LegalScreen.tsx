"use client";

import { PRIVACY_VERSION, TERMS_VERSION, useMe } from "@scaffoldapp/shared";
import { Card, ListRow } from "@scaffoldapp/ui-web";
import { FileText, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { Screen } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

/** Settings → Legal: the two documents, marked "Accepted" when this account accepted the current version. */
export function LegalScreen() {
  const me = useMe();
  const consent = me.data?.consent;
  const rows = [
    { href: "/legal/terms", label: "Terms of Service", icon: FileText, accepted: consent?.accepted_terms, current: TERMS_VERSION },
    { href: "/legal/privacy", label: "Privacy Policy", icon: ShieldCheck, accepted: consent?.accepted_privacy, current: PRIVACY_VERSION },
  ];
  return (
    <Screen title="Legal">
      <Card padding="none" className="p-1">
        <ul className="flex flex-col">
          {rows.map((r) => {
            const Icon = r.icon;
            return (
              <li key={r.href}>
                <ListRow
                  asChild
                  title={r.label}
                  leading={
                    <span className="flex size-8 items-center justify-center rounded-md bg-sunken text-fg">
                      <Icon className="size-4" aria-hidden />
                    </span>
                  }
                  trailing={<span className="type-caption">{r.accepted === r.current ? "Accepted" : `Version ${formatDate(r.current)}`}</span>}
                >
                  <Link href={r.href} />
                </ListRow>
              </li>
            );
          })}
        </ul>
      </Card>
    </Screen>
  );
}
