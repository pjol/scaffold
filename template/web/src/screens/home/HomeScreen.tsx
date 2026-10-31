"use client";

import { useMe } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, Stagger, StaggerItem, badgeText } from "@scaffoldapp/ui-web";
import { LayoutGrid } from "lucide-react";
import Link from "next/link";
import { useNavBadges } from "@/features/nav";
import { homeTiles } from "@/features/registry";

function firstName(displayName: string | undefined, email: string | undefined): string {
  const name = displayName?.trim().split(/\s+/)[0];
  return name || email?.split("@")[0] || "";
}

/**
 * Home (`/`): a greeting and one tile per registry `homeTiles` entry.
 * With no sections installed it says so and offers the profile; it never shows
 * placeholder content. Replace or extend it with the product's real first screen.
 */
export function HomeScreen() {
  const me = useMe();
  const badges = useNavBadges(homeTiles);
  const name = firstName(me.data?.display_name, me.data?.email);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <h1 className="type-title text-fg lg:type-display">{name ? `Hi, ${name}` : "Hi"}</h1>
      {homeTiles.length === 0 ? (
        <Card padding="compact">
          <EmptyState
            icon={<LayoutGrid />}
            title="No sections yet"
            action={
              <Button asChild variant="secondary" size="auto">
                <Link href="/settings/profile">Edit your profile</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        <Stagger as="ul" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-label="Sections">
          {homeTiles.map((item, i) => {
            const Icon = item.icon;
            const badge = badges[i];
            return (
              <StaggerItem as="li" key={item.href}>
                <Card interactive asChild>
                  <Link href={item.href} className="flex min-h-tap-xl items-center gap-4">
                    <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary-strong">
                      <Icon className="size-5" aria-hidden />
                    </span>
                    <span className="min-w-0 flex-1 truncate type-title-sm text-fg">{item.label}</span>
                    {badge ? (
                      <Badge tone="primary" variant="solid" pop key={badge}>
                        {badgeText(badge)}
                      </Badge>
                    ) : null}
                  </Link>
                </Card>
              </StaggerItem>
            );
          })}
        </Stagger>
      )}
    </div>
  );
}
