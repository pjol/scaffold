"use client";

import { Button, Card, ListRow } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { Fragment } from "react";
import { supportLinks } from "@/features/registry";

/**
 * The registry's `supportLinks`, in the shape each place needs: `buttons` (consent and
 * blocked-account screens), `inline` (inside a banner sentence), `list` (Settings →
 * Support). Renders nothing when no feature adds one.
 */
export function SupportLinks({ variant }: { variant: "buttons" | "inline" | "list" }) {
  if (supportLinks.length === 0) return null;
  if (variant === "inline") {
    return (
      <>
        {supportLinks.map((l) => (
          <Fragment key={l.id}>
            {" "}
            <Link className="underline underline-offset-2" href={l.href}>
              {l.label}
            </Link>
          </Fragment>
        ))}
      </>
    );
  }
  if (variant === "buttons") {
    return (
      <>
        {supportLinks.map((l) => {
          const Icon = l.icon;
          return (
            <Button key={l.id} asChild variant="ghost" size="auto" leftIcon={<Icon />}>
              <Link href={l.href}>{l.label}</Link>
            </Button>
          );
        })}
      </>
    );
  }
  return (
    <Card padding="none" className="p-1">
      <ul className="flex flex-col">
        {supportLinks.map((l) => {
          const Icon = l.icon;
          return (
            <li key={l.id}>
              <ListRow
                asChild
                title={l.label}
                leading={
                  <span className="flex size-8 items-center justify-center rounded-md bg-sunken text-fg">
                    <Icon className="size-4" aria-hidden />
                  </span>
                }
              >
                <Link href={l.href} />
              </ListRow>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}
