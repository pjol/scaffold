import {
  connectorCapabilitiesByFeature,
  connectorCapabilityState,
  connectorPlatformAvailable,
  connectorPlatformDisconnectedMessage,
  connectorPlatformErrorMessage,
  connectorTokenLabel,
  errorMessage,
  useActiveOrg,
  useConnectConnectorPlatform,
  useConnectorPlatforms,
  useDisconnectConnectorPlatform,
  useFinishConnectorPlatform,
  useSetConnectorPlatformCapability,
  type ConnectorPlatform,
  type ConnectorPlatformCapability,
  type ConnectorPlatformConnection,
} from "@scaffoldapp/shared";
import { Badge, Body, Button, ConfirmDialog, ErrorState, ListGroup, ListRow, ProviderIcon, Screen, Skeleton, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import * as Linking from "expo-linking";
import { router, useLocalSearchParams, type Href } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { LogIn, Unplug } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AssistantToolsSection, RETURN_URL } from "./AssistantTools";

type Params = { platform_finish?: string; finish?: string; connect_error?: string; capability?: string; return_to?: string };

function safeReturn(raw: string | undefined | null): string | null {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.includes("\\")) return null;
  return raw;
}

/**
 * Settings → Connections on mobile, by platform: each account with its status and a ⋮ menu
 * (Reconnect, Disconnect, AI agent access), a switch per capability, and Connect through
 * the system browser (an auth session that returns here). Personal tokens are added on the
 * web. `?capability=<key>&return_to=<path>` puts that platform first with the capability
 * marked, and returns afterwards. The assistant's MCP tools follow.
 */
export function ConnectionsScreen() {
  const theme = useTheme();
  const params = useLocalSearchParams<Params>();
  const { org } = useActiveOrg();
  const orgId = org?.id ?? null;
  const list = useConnectorPlatforms(orgId);
  const connect = useConnectConnectorPlatform();
  const setCap = useSetConnectorPlatformCapability();
  const finish = useFinishConnectorPlatform();
  const disconnect = useDisconnectConnectorPlatform();
  const [busy, setBusy] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState<{ platform: ConnectorPlatform; conn: ConnectorPlatformConnection } | null>(null);
  const handled = useRef(new Set<string>());
  const returnTo = safeReturn(params.return_to);

  const complete = useCallback(
    (secret: string | undefined, error: string | undefined) => {
      const key = secret ?? error;
      if (!key || handled.current.has(key)) return;
      handled.current.add(key);
      if (error) {
        toast.error(connectorPlatformErrorMessage("The platform", error));
        return;
      }
      if (secret) {
        finish.mutate(
          { secret, orgId },
          {
            onSuccess: (f) => {
              toast.success(f.capability && !f.capability_enabled ? `Connected ${f.connection.account}, but not everything asked for was allowed` : `Connected ${f.connection.account}`);
              const back = safeReturn(f.return_to);
              if (back) router.replace(back as Href);
            },
            onError: (e) => toast.error(errorMessage(e, "Couldn't finish connecting. Try again.")),
          },
        );
      }
    },
    [finish, orgId],
  );
  // Android also routes the return link here; iOS hands it to the auth session only.
  useEffect(() => complete(params.platform_finish, undefined), [params.platform_finish, complete]);

  const openAuth = async (url: string) => {
    const out = await WebBrowser.openAuthSessionAsync(url, RETURN_URL);
    if (out.type !== "success") return;
    const q = Linking.parse(out.url).queryParams ?? {};
    const pick = (v: unknown) => (typeof v === "string" ? v : undefined);
    complete(pick(q.platform_finish), pick(q.connect_error));
  };

  const signIn = async (p: ConnectorPlatform, opts: { capability?: string; connectionId?: string } = {}) => {
    setBusy(`${p.id}:${opts.capability ?? ""}:${opts.connectionId ?? ""}`);
    try {
      const r = await connect.mutateAsync({
        provider: p.id,
        client: "mobile",
        ...(opts.capability ? { capability: opts.capability } : {}),
        ...(opts.connectionId ? { connection_id: opts.connectionId } : {}),
        ...(returnTo ? { return_to: returnTo } : {}),
        ...(orgId ? { org_id: orgId } : {}),
      });
      if (r.authorize_url) await openAuth(r.authorize_url);
      else if (r.connection) toast.success(`Turned on for ${r.connection.account}`);
    } catch (e) {
      toast.error(errorMessage(e, `Couldn't start connecting ${p.name}. Try again.`));
    } finally {
      setBusy(null);
    }
  };

  const toggle = async (conn: ConnectorPlatformConnection, c: ConnectorPlatformCapability, enabled: boolean) => {
    try {
      const r = await setCap.mutateAsync({ connectionId: conn.id, capability: c.key, enabled, client: "mobile", ...(returnTo ? { returnTo } : {}), ...(orgId ? { orgId } : {}) });
      if (r.authorize_url) await openAuth(r.authorize_url);
      else if (enabled && returnTo && c.key === params.capability) router.replace(returnTo as Href);
    } catch (e) {
      toast.error(errorMessage(e, `Couldn't change ${c.label}. Try again.`));
    }
  };

  const items = list.data?.items ?? [];
  const focus = params.capability ? items.find((p) => p.capabilities.some((c) => c.key === params.capability)) : undefined;
  const ordered = focus ? [focus, ...items.filter((p) => p !== focus)] : items;

  return (
    <Screen edges={["bottom"]} gap={theme.space[6]}>
      {list.isPending ? (
        <View style={{ gap: theme.space[4] }}>
          <Skeleton height={theme.tap.base * 2} />
          <Skeleton height={theme.tap.base * 3} />
        </View>
      ) : list.isError ? (
        <ErrorState title="Couldn't load connections" onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : (
        ordered.map((p) => (
          <PlatformGroup
            key={p.id}
            platform={p}
            focusCapability={focus === p ? params.capability : undefined}
            busy={busy}
            onSignIn={(o) => void signIn(p, o)}
            onToggle={(conn, c, v) => void toggle(conn, c, v)}
            onDisconnect={(conn) => setDisconnecting({ platform: p, conn })}
          />
        ))
      )}

      <AssistantToolsSection mcpFinish={params.finish} mcpError={params.platform_finish ? undefined : params.connect_error} />

      <ConfirmDialog
        open={disconnecting !== null}
        onClose={() => setDisconnecting(null)}
        title={`Disconnect ${disconnecting?.conn.account ?? ""}?`}
        confirmLabel="Disconnect"
        tone="danger"
        onConfirm={async () => {
          const d = disconnecting;
          if (!d) return;
          const r = await disconnect.mutateAsync(d.conn.id);
          toast.success(connectorPlatformDisconnectedMessage(d.platform.name, d.conn.account, r));
        }}
        errorText={(e) => errorMessage(e, "Couldn't disconnect. Try again.")}
      >
        <Body>{`Every feature using this ${disconnecting?.platform.name ?? ""} account stops showing its content.`}</Body>
      </ConfirmDialog>
    </Screen>
  );
}

function PlatformGroup({
  platform: p,
  focusCapability,
  busy,
  onSignIn,
  onToggle,
  onDisconnect,
}: {
  platform: ConnectorPlatform;
  focusCapability?: string | undefined;
  busy: string | null;
  onSignIn: (o?: { capability?: string; connectionId?: string }) => void;
  onToggle: (conn: ConnectorPlatformConnection, c: ConnectorPlatformCapability, enabled: boolean) => void;
  onDisconnect: (conn: ConnectorPlatformConnection) => void;
}) {
  const theme = useTheme();
  const groups = connectorCapabilitiesByFeature(p.capabilities);
  const available = connectorPlatformAvailable(p);
  const icon = <ProviderIcon provider={p.id} size={20} />;
  if (!p.policy.allowed) {
    return (
      <ListGroup title={p.name}>
        <ListRow title="Blocked by your workspace" leading={icon} info={p.capabilities.find((c) => c.blocked_reason)?.blocked_reason ?? "Your workspace doesn't allow this platform."} chevron={false} />
      </ListGroup>
    );
  }
  const focusLabel = focusCapability ? p.capabilities.find((c) => c.key === focusCapability)?.label : undefined;
  return (
    <ListGroup title={p.name} {...(!available ? { info: `${p.name} isn't set up on this server yet.` } : {})}>
      {p.connections.map((conn) => (
        <View key={conn.id}>
          <ListRow
            title={conn.account}
            leading={icon}
            chevron={false}
            {...(conn.last_error ? { info: conn.last_error } : {})}
            trailing={
              <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
                {conn.status === "needs_reauth" ? (
                  <Badge size="sm" dot tone="warning">
                    Sign in again
                  </Badge>
                ) : conn.status === "blocked" ? (
                  <Badge size="sm">Blocked</Badge>
                ) : conn.kind === "token" ? (
                  <Badge size="sm">Token</Badge>
                ) : null}
                <ResourceMenu
                  feature={conn.agent_scope}
                  kind="connectors.platform_connection"
                  id={conn.id}
                  title={`${p.name} · ${conn.account}`}
                  label={`${p.name} · ${conn.account} options`}
                  actions={[
                    ...(conn.kind === "oauth" && p.oauth ? [{ id: "reconnect", label: "Reconnect", icon: LogIn, onSelect: () => onSignIn({ connectionId: conn.id }) }] : []),
                    { id: "disconnect", label: "Disconnect", icon: Unplug, destructive: true, onSelect: () => onDisconnect(conn) },
                  ]}
                />
              </View>
            }
          />
          {conn.status !== "blocked"
            ? groups.flatMap((g) =>
                g.capabilities.map((c) => {
                  const state = connectorCapabilityState(conn, c.key);
                  return (
                    <View key={c.key} style={{ paddingHorizontal: theme.space[4], backgroundColor: c.key === focusCapability ? theme.colors.primarySoft : undefined }}>
                      <Switch
                        label={c.label}
                        info={`${g.label}. ${c.write ? `Can change data in ${p.name}.` : `Reads from ${p.name}.`} Asks for: ${c.scopes.join(", ") || "nothing extra"}.${c.scope_notes
                          .map((n) => ` ${n.scope}: ${n.note}.`)
                          .join("")}${state === "needs_grant" ? " Needs access: switch it off and on to allow it." : ""}${c.blocked_reason ? ` ${c.blocked_reason}` : ""}`}
                        value={state === "on" || state === "needs_grant"}
                        disabled={state === "blocked" || conn.status !== "active"}
                        onValueChange={(v) => onToggle(conn, c, v)}
                        accessibilityLabel={`${c.label} for ${conn.account}`}
                      />
                    </View>
                  );
                }),
              )
            : null}
        </View>
      ))}
      {available && p.oauth ? (
        focusLabel && p.connections.length === 0 ? (
          <View style={{ padding: theme.space[4] }}>
            <Button onPress={() => onSignIn({ capability: focusCapability! })} loading={busy === `${p.id}:${focusCapability}:`}>
              {`Connect ${focusLabel}`}
            </Button>
          </View>
        ) : (
          <ListRow
            title={p.connections.length > 0 ? "Add another account" : `Connect ${p.name}`}
            {...(p.connections.length > 0 ? {} : { leading: icon })}
            trailing={busy === `${p.id}::` ? <Text tone="muted">Opening…</Text> : <Text tone="primary">Connect</Text>}
            chevron={false}
            disabled={busy !== null}
            onPress={() => onSignIn()}
          />
        )
      ) : available && p.token ? (
        <ListRow title={`Add ${connectorTokenLabel(p.token.label)}`} leading={icon} info="Personal tokens are added in Settings → Connections on the web." trailing={<Text tone="muted">On the web</Text>} chevron={false} />
      ) : p.connections.length === 0 ? (
        <ListRow title="Not set up" leading={icon} chevron={false} />
      ) : null}
    </ListGroup>
  );
}
