"use client";

import { Card, ListRow } from "@scaffoldapp/ui-web";
import { FileText, ShieldCheck, Trash2 } from "lucide-react";
import Link from "next/link";
import { SupportContact } from "@/components/account/SupportContact";

const tile = "flex size-8 items-center justify-center rounded-md bg-sunken text-fg";

/** /support: the published contact (stores require it), plus the legal and deletion pages. */
export function SupportPageScreen() {
  return (
    <div className="mx-auto flex w-full max-w-prose flex-col gap-6 px-gutter py-section">
      <h1 className="type-title text-fg lg:type-display">Support</h1>
      <div className="flex flex-col gap-section">
        <SupportContact />
        <Card padding="none" className="p-1">
          <ul className="flex flex-col">
            {[
              { href: "/legal/terms", label: "Terms of Service", Icon: FileText },
              { href: "/legal/privacy", label: "Privacy Policy", Icon: ShieldCheck },
              { href: "/delete-account", label: "Delete your account", Icon: Trash2 },
            ].map(({ href, label, Icon }) => (
              <li key={href}>
                <ListRow
                  asChild
                  title={label}
                  leading={
                    <span className={tile}>
                      <Icon className="size-4" aria-hidden />
                    </span>
                  }
                >
                  <Link href={href} />
                </ListRow>
              </li>
            ))}
          </ul>
        </Card>
      </div>
    </div>
  );
}
