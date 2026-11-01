"use client";

import {
  AGENTS_LEVEL_INFO,
  AGENTS_LEVEL_LABEL,
  AGENTS_LEVELS,
  AGENTS_LEVELS_INFO,
  errorMessage,
  useAgentsRemoveResourceScope,
  useAgentsResourceScopes,
  useAgentsServices,
  useAgentsSetScope,
  useAgentsSettings,
  useAgentsUpdateSettings,
  type AgentsFeatureScope,
  type AgentsLevel,
  type AgentsSettings,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  IconButton,
  InfoTip,
  ListRow,
  SegmentedControl,
  Select,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  toast,
} from "@scaffoldapp/ui-web";
import { Bot, Inbox, Layers, Plug, Power, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen, Section } from "@/components/shell/PageHeader";
import { settingsSections } from "@/features/registry";
import { formatRelative } from "@/lib/format";
import { LEVEL_TONE, VIA_LABEL, agentIcon } from "./labels";

/** Where each agent feature is managed, shown only when that settings page exists in this app. */
const MANAGE_LINKS = ["/settings/connected-apps", "/settings/api-keys", "/settings/assistant", "/settings/connections"];

function toolsInfo(f: AgentsFeatureScope): string {
  const parts = [`${f.read_tools} ${f.read_tools === 1 ? "tool reads" : "tools read"}`];
  if (f.write_tools) parts.push(`${f.write_tools} ${f.write_tools === 1 ? "tool makes" : "tools make"} changes`);
  let s = parts.join(", ") + ".";
  if (f.resource_overrides) s += ` ${f.resource_overrides} ${f.resource_overrides === 1 ? "item has" : "items have"} its own setting.`;
  if (!f.connected) s += " Not connected now; the setting is kept for when you reconnect.";
  return s;
}

function LevelSelect({ f, defaultLevel }: { f: AgentsFeatureScope; defaultLevel: AgentsLevel }) {
  const set = useAgentsSetScope();
  return (
    <Select
      aria-label={`AI agent access for ${f.label}`}
      className="w-40"
      value={f.level ?? "default"}
      disabled={set.isPending}
      onValueChange={(v) =>
        set.mutate(
          { feature: f.feature, level: v === "default" ? null : (v as AgentsLevel), title: f.label },
          { onError: (e) => toast.error(errorMessage(e, "Couldn't change it. Try again.")) },
        )
      }
      options={[
        { value: "default", label: `Default (${AGENTS_LEVEL_LABEL[defaultLevel]})` },
        ...AGENTS_LEVELS.map((l) => ({ value: l, label: AGENTS_LEVEL_LABEL[l] })),
      ]}
    />
  );
}

/** Name (+ⓘ) and level picker; on narrow screens the picker wraps under the name instead of cutting it off. */
function FeatureList({ items, defaultLevel }: { items: AgentsFeatureScope[]; defaultLevel: AgentsLevel }) {
  return (
    <ul className="flex flex-col">
      {items.map((f) => (
        <li key={f.feature} className="flex min-h-row flex-wrap items-center gap-x-3 gap-y-2 rounded-md px-4 py-2">
          <span className="flex min-w-40 flex-1 items-center gap-2">
            <span className="type-body font-medium text-fg">{f.label}</span>
            <InfoTip content={toolsInfo(f)} />
          </span>
          <LevelSelect f={f} defaultLevel={defaultLevel} />
        </li>
      ))}
    </ul>
  );
}

function Services({ settings }: { settings: AgentsSettings }) {
  const q = useAgentsServices();
  const connectLink = settingsSections.find((s) => s.href === "/settings/connections");
  return (
    <Section title="Connected services" info="Outside services you connected (through Connections) that the assistant can use for you. Each has its own level.">
      <Card padding="none" className="p-1">
        {q.isPending ? (
          <SkeletonGroup label="Loading services" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : q.isError ? (
          <ErrorState compact title="Couldn't load your services" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        ) : q.data.items.length === 0 ? (
          <EmptyState
            compact
            icon={<Plug />}
            title="No services connected"
            action={
              connectLink ? (
                <Button asChild variant="secondary" size="auto">
                  <Link href={connectLink.href}>Connect a service</Link>
                </Button>
              ) : undefined
            }
          />
        ) : (
          <FeatureList items={q.data.items} defaultLevel={settings.default_level} />
        )}
      </Card>
    </Section>
  );
}

function ItemOverrides() {
  const q = useAgentsResourceScopes();
  const remove = useAgentsRemoveResourceScope();
  const rows = q.data?.items ?? [];
  return (
    <Section
      title="Items with their own setting"
      info="Set from an item's ⋮ menu → AI agent access. A folder's setting also covers everything inside it, unless an item inside has its own."
    >
      <Card padding="none" className="p-1">
        {q.isPending ? (
          <SkeletonGroup label="Loading items" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : q.isError ? (
          <ErrorState compact title="Couldn't load your item settings" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Layers />} title="No items have their own setting" />
        ) : (
          <ul className="flex flex-col">
            {rows.map((r) => {
              const title = r.missing ? `Unavailable ${r.noun}` : r.title || `Untitled ${r.noun}`;
              return (
                <li key={r.id}>
                  <ListRow
                    title={
                      r.link && !r.missing ? (
                        <Link href={r.link} className="underline-offset-2 hover:underline">
                          {title}
                        </Link>
                      ) : (
                        title
                      )
                    }
                    info={
                      r.missing
                        ? "It was deleted or you can no longer open it. Remove the setting."
                        : `${r.feature_label} ${r.noun}.${r.inherits ? " Covers what's inside it." : ""}`
                    }
                    trailing={
                      <span className="flex items-center gap-2">
                        <Badge tone={LEVEL_TONE[r.level]}>{AGENTS_LEVEL_LABEL[r.level]}</Badge>
                        <IconButton
                          label={`Remove the setting for ${title}`}
                          icon={<X />}
                          variant="ghost"
                          size="sm"
                          disabled={remove.isPending}
                          onClick={() => remove.mutate(r.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't remove it. Try again.")) })}
                        />
                      </span>
                    }
                  />
                </li>
              );
            })}
          </ul>
        )}
        <LoadMore hasNextPage={q.hasNextPage} isFetchingNextPage={q.isFetchingNextPage} fetchNextPage={q.fetchNextPage} />
      </Card>
    </Section>
  );
}

function Agents({ settings }: { settings: AgentsSettings }) {
  const links = settingsSections.filter((s) => MANAGE_LINKS.includes(s.href));
  return (
    <Section title="Agents" info="AI agents that have acted for you. Connect, disconnect or revoke them where they're managed.">
      <Card padding="none" className="p-1">
        {settings.agents.length === 0 ? (
          <EmptyState compact icon={<Bot />} title="No agent has acted for you yet" />
        ) : (
          <ul className="flex flex-col">
            {settings.agents.map((a) => {
              const Icon = agentIcon(a.via);
              return (
                <li key={`${a.via}|${a.client_key}`}>
                  <ListRow
                    title={a.client}
                    leading={<Icon className="size-5 text-fg-muted" aria-hidden />}
                    info={`${VIA_LABEL[a.via] ?? a.via}. First seen ${formatRelative(a.created_at)}.`}
                    trailing={
                      <span className="flex items-center gap-2">
                        {a.pending > 0 && <Badge tone="warning">{a.pending} waiting</Badge>}
                        <span className="hidden type-caption sm:inline">Active {formatRelative(a.last_seen_at)}</span>
                      </span>
                    }
                  />
                </li>
              );
            })}
          </ul>
        )}
      </Card>
      {links.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {links.map((l) => {
            const Icon = l.icon;
            return (
              <Button key={l.href} asChild variant="secondary" size="sm" leftIcon={<Icon />}>
                <Link href={l.href}>{l.label}</Link>
              </Button>
            );
          })}
        </div>
      )}
    </Section>
  );
}

function SettingsSkeleton() {
  return (
    <SkeletonGroup label="Loading AI agent settings" className="flex flex-col gap-section">
      <Skeleton className="h-12 w-full max-w-form rounded-lg" />
      <Skeleton className="h-48 w-full rounded-lg" />
      <Skeleton className="h-32 w-full rounded-lg" />
    </SkeletonGroup>
  );
}

/** Settings → AI agents: default level, per feature and service, items, agents, and the off switch. */
export function AgentsSettingsScreen() {
  const q = useAgentsSettings();
  const update = useAgentsUpdateSettings();
  const [confirmOff, setConfirmOff] = useState(false);
  const s = q.data;

  const setPaused = (paused: boolean) =>
    update.mutateAsync({ paused }).then(
      () => toast.success(paused ? "All AI agent access is off" : "AI agent access is back on"),
      (e: unknown) => {
        toast.error(errorMessage(e, "Couldn't change it. Try again."));
        throw e;
      },
    );

  return (
    <Screen title="AI agents" info="What AI agents acting for you may do: apps you connect (Claude, ChatGPT, scripts with your API key) and the in-app assistant. They never get more access than you have.">
      {q.isPending ? (
        <SettingsSkeleton />
      ) : q.isError ? (
        <Card>
          <ErrorState compact title="Couldn't load your AI agent settings" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        </Card>
      ) : s ? (
        <>
          {s.paused_by_staff ? (
            <Banner tone="danger">Support turned off AI agent access for this account. Contact support to turn it back on.</Banner>
          ) : s.paused ? (
            <Banner
              tone="warning"
              action={
                <Button size="sm" loading={update.isPending} onClick={() => void setPaused(false).catch(() => undefined)}>
                  Turn back on
                </Button>
              }
            >
              All AI agent access is off. Your settings are kept.
            </Banner>
          ) : null}

          <Card padding="none" className="p-1">
            <ListRow
              asChild
              title="Suggestions"
              leading={<Inbox className="size-5 text-fg-muted" aria-hidden />}
              trailing={s.pending_suggestions > 0 ? <Badge tone="warning">{s.pending_suggestions} waiting</Badge> : undefined}
            >
              <Link href="/agents/suggestions" />
            </ListRow>
          </Card>

          <Section title="Default" info={AGENTS_LEVELS_INFO}>
            <SegmentedControl<AgentsLevel>
              aria-label="Default AI agent access"
              className="max-w-form"
              value={s.default_level}
              onValueChange={(level) =>
                update.mutate({ default_level: level }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the default. Try again.")) })
              }
              options={AGENTS_LEVELS.map((l) => ({ value: l, label: AGENTS_LEVEL_LABEL[l] }))}
            />
            <p className="sr-only" aria-live="polite">
              {AGENTS_LEVEL_INFO[s.default_level]}
            </p>
          </Section>

          <Section title="Features" info="A feature's own level wins over the default; an item's own level wins over its feature's. Parts that serve the whole app, like search and notifications, aren't listed and follow the default.">
            <Card padding="none" className="p-1">
              {s.features.length === 0 ? (
                <EmptyState compact icon={<Bot />} title="No features give agents tools yet" />
              ) : (
                <FeatureList items={s.features} defaultLevel={s.default_level} />
              )}
            </Card>
          </Section>

          {s.surfaces.includes("connectors") && <Services settings={s} />}

          <ItemOverrides />

          <Agents settings={s} />

          {!s.paused && !s.paused_by_staff && (
            <Section title="Turn off" info="Stops every agent at once: nothing can be read or suggested until you turn it back on. Your levels are kept.">
              <div>
                <Button variant="danger-ghost" size="auto" leftIcon={<Power />} onClick={() => setConfirmOff(true)}>
                  Turn off all agent access
                </Button>
              </div>
            </Section>
          )}
          <ConfirmDialog
            open={confirmOff}
            onOpenChange={setConfirmOff}
            title="Turn off all AI agent access?"
            confirmLabel="Turn off"
            tone="danger"
            onConfirm={() => setPaused(true)}
          >
            Agents connected to your account can't read, suggest or change anything until you turn it back on. Your settings are kept.
          </ConfirmDialog>
        </>
      ) : null}
    </Screen>
  );
}
