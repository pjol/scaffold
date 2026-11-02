import {
  AGENTS_LEVEL_LABEL,
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
  type AgentsResourceScope,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, Screen, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { Bot, Inbox, Layers, Plug, Power } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { settingsSections } from "../registry";
import { agentIcon, LEVEL_TONE, relativeTime, VIA_LABEL } from "./labels";
import { LevelSheet } from "./LevelSheet";

/** Where agent features are managed on mobile, when this app ships that screen. */
const MANAGE_LINKS = ["/settings/assistant", "/settings/connections"];

type Picking =
  | { type: "default" }
  | { type: "feature"; f: AgentsFeatureScope }
  | { type: "item"; r: AgentsResourceScope }
  | null;

function toolsInfo(f: AgentsFeatureScope): string {
  if (f.platform) {
    return `Caps what AI agents read and change in this account through every feature; each feature's own level applies too.${f.connected ? "" : " Not connected now; the setting is kept."}`;
  }
  let s = `${f.read_tools} ${f.read_tools === 1 ? "tool reads" : "tools read"}`;
  if (f.write_tools) s += `, ${f.write_tools} ${f.write_tools === 1 ? "tool makes" : "tools make"} changes`;
  s += ".";
  if (f.resource_overrides) s += ` ${f.resource_overrides} ${f.resource_overrides === 1 ? "item has" : "items have"} its own setting.`;
  if (!f.connected) s += " Not connected now; the setting is kept.";
  return s;
}

/** Settings → AI agents: default, features, services, items, agents, and the off switch. */
export function AgentsSettingsScreen() {
  const theme = useTheme();
  const s = theme.space;
  const q = useAgentsSettings();
  const settings = q.data;
  const services = useAgentsServices(!!settings?.surfaces.includes("connectors"));
  const items = useAgentsResourceScopes();
  const update = useAgentsUpdateSettings();
  const setScope = useAgentsSetScope();
  const remove = useAgentsRemoveResourceScope();
  const [picking, setPicking] = useState<Picking>(null);
  const [confirmOff, setConfirmOff] = useState(false);
  const links = settingsSections.flatMap((g) => g.items).filter((i) => MANAGE_LINKS.includes(String(i.href)));

  const fail = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const setFeature = (f: AgentsFeatureScope, level: AgentsLevel | null) =>
    setScope.mutate({ feature: f.feature, level, title: f.label }, { onError: fail("Couldn't change it. Try again.") });
  const setPaused = (paused: boolean) =>
    update.mutateAsync({ paused }).then(
      () => toast.success(paused ? "All AI agent access is off" : "AI agent access is back on"),
      (e: unknown) => {
        fail("Couldn't change it. Try again.")(e);
        throw e;
      },
    );

  const featureRow = (f: AgentsFeatureScope, def: AgentsLevel) => (
    <ListRow
      key={f.feature}
      title={f.label}
      info={toolsInfo(f)}
      trailing={<Text tone="muted">{f.level ? AGENTS_LEVEL_LABEL[f.level] : `Default (${AGENTS_LEVEL_LABEL[def]})`}</Text>}
      onPress={() => setPicking({ type: "feature", f })}
    />
  );

  return (
    <Screen
      edges={["bottom"]}
      gap={s[8]}
      refreshControl={
        <RefreshControl
          refreshing={q.isRefetching}
          onRefresh={() => {
            void q.refetch();
            void items.refetch();
            if (settings?.surfaces.includes("connectors")) void services.refetch();
          }}
        />
      }
    >
      <Stack.Screen options={{ title: "AI agents" }} />
      {q.isPending ? (
        <ListGroup>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </ListGroup>
      ) : q.isError ? (
        <ErrorState title="Couldn't load your AI agent settings" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      ) : settings ? (
        <>
          {settings.paused_by_staff ? (
            <Banner tone="danger" title="Support turned off AI agent access for this account. Contact support to turn it back on." />
          ) : settings.paused ? (
            <Banner tone="warning" title="All AI agent access is off. Your settings are kept." action={{ label: "Turn on", onPress: () => void setPaused(false).catch(() => undefined) }} />
          ) : null}

          <ListGroup>
            <ListRow
              title="Suggestions"
              leading={<Inbox size={20} color={theme.colors.text} />}
              trailing={settings.pending_suggestions > 0 ? <Badge tone="warning">{`${settings.pending_suggestions} waiting`}</Badge> : undefined}
              onPress={() => router.push("/agents/suggestions" as Href)}
            />
          </ListGroup>

          <ListGroup title="Default" info={AGENTS_LEVELS_INFO}>
            <ListRow
              title="Default level"
              trailing={<Text tone="muted">{AGENTS_LEVEL_LABEL[settings.default_level]}</Text>}
              onPress={() => setPicking({ type: "default" })}
            />
          </ListGroup>

          <ListGroup title="Features" info="A feature's own level wins over the default; an item's own level wins over its feature's. Parts that serve the whole app, like search and notifications, aren't listed and follow the default.">
            {settings.features.length === 0 ? (
              <EmptyState compact icon={Bot} title="No features give agents tools yet" />
            ) : (
              settings.features.map((f) => featureRow(f, settings.default_level))
            )}
          </ListGroup>

          {settings.surfaces.includes("connectors") ? (
            <ListGroup title="Connections" info="Accounts and services you connected in Connections. Each has its own level: an account's level caps what agents do there through every feature.">
              {services.isPending ? (
                <SkeletonRow />
              ) : services.isError ? (
                <ErrorState compact title="Couldn't load your services" onRetry={() => void services.refetch()} retrying={services.isFetching} />
              ) : services.data.items.length === 0 ? (
                <EmptyState compact icon={Plug} title="Nothing connected" />
              ) : (
                services.data.items.map((f) => featureRow(f, settings.default_level))
              )}
            </ListGroup>
          ) : null}

          <ListGroup title="Items with their own setting" info="Set from an item's ⋮ menu → AI agent access. A folder's setting covers what's inside it, unless an item inside has its own.">
            {items.isPending ? (
              <SkeletonRow />
            ) : items.isError ? (
              <ErrorState compact title="Couldn't load your item settings" onRetry={() => void items.refetch()} retrying={items.isFetching} />
            ) : (items.data?.items ?? []).length === 0 ? (
              <EmptyState compact icon={Layers} title="No items have their own setting" />
            ) : (
              (items.data?.items ?? []).map((r) => (
                <ListRow
                  key={r.id}
                  title={r.missing ? `Unavailable ${r.noun}` : r.title || `Untitled ${r.noun}`}
                  trailing={<Badge tone={LEVEL_TONE[r.level]}>{AGENTS_LEVEL_LABEL[r.level]}</Badge>}
                  onPress={() => setPicking({ type: "item", r })}
                />
              ))
            )}
            {items.hasNextPage ? (
              <View style={{ padding: s[2] }}>
                <Button variant="ghost" loading={items.isFetchingNextPage} onPress={() => void items.fetchNextPage()}>
                  Show more
                </Button>
              </View>
            ) : null}
          </ListGroup>

          <ListGroup title="Agents" info="AI agents that have acted for you. Connect, disconnect or revoke them where they're managed.">
            {settings.agents.length === 0 ? (
              <EmptyState compact icon={Bot} title="No agent has acted for you yet" />
            ) : (
              settings.agents.map((a) => {
                const Icon = agentIcon(a.via);
                return (
                  <ListRow
                    key={`${a.via}|${a.client_key}`}
                    title={a.client}
                    leading={<Icon size={20} color={theme.colors.textMuted} />}
                    info={`${VIA_LABEL[a.via] ?? a.via}. Last active ${relativeTime(a.last_seen_at)}.`}
                    trailing={a.pending > 0 ? <Badge tone="warning">{`${a.pending} waiting`}</Badge> : undefined}
                  />
                );
              })
            )}
            {links.map((l) => (
              <ListRow key={String(l.href)} title={l.title} leading={<l.icon size={20} color={theme.colors.text} />} onPress={() => router.push(l.href)} />
            ))}
          </ListGroup>

          {!settings.paused && !settings.paused_by_staff ? (
            <ListGroup info="Stops every agent at once: nothing can be read or suggested until you turn it back on. Your levels are kept.">
              <ListRow title="Turn off all agent access" leading={<Power size={20} color={theme.colors.dangerStrong} />} destructive chevron={false} onPress={() => setConfirmOff(true)} />
            </ListGroup>
          ) : null}

          <LevelSheet
            open={picking !== null}
            onClose={() => setPicking(null)}
            title={picking?.type === "feature" ? picking.f.label : picking?.type === "item" ? picking.r.title || `Untitled ${picking.r.noun}` : "Default level"}
            value={picking?.type === "feature" ? picking.f.level : picking?.type === "item" ? picking.r.level : settings.default_level}
            inherited={picking?.type === "feature" ? { level: settings.default_level, source: "your default" } : undefined}
            onChoose={(level) => {
              if (!picking) return;
              if (picking.type === "default") {
                if (level) update.mutate({ default_level: level }, { onError: fail("Couldn't change the default. Try again.") });
              } else if (picking.type === "feature") setFeature(picking.f, level);
              else if (level) setScope.mutate({ feature: picking.r.feature, kind: picking.r.kind, id: picking.r.resource_id, level }, { onError: fail("Couldn't change it. Try again.") });
            }}
            remove={
              picking?.type === "item"
                ? { label: "Remove its own setting", onPress: () => remove.mutate(picking.r.id, { onError: fail("Couldn't remove it. Try again.") }) }
                : undefined
            }
            open_={picking?.type === "item" && picking.r.link && !picking.r.missing ? { label: `Open ${picking.r.noun}`, onPress: () => router.push(picking.r.link as Href) } : undefined}
          />
          <ConfirmDialog
            open={confirmOff}
            onClose={() => setConfirmOff(false)}
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
