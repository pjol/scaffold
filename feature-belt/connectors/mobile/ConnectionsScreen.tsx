import {
  connectorConnectErrorMessage,
  connectorDisconnectedMessage,
  connectorFeatureKey,
  connectorState,
  connectorStateLabel,
  errorMessage,
  useConnectors,
  useDisconnectConnector,
  useFinishConnectorConnect,
  useStartConnectorConnect,
  type Connector,
} from "@scaffoldapp/shared";
import { Badge, Body, ConfirmDialog, EmptyState, ErrorState, ListGroup, ListRow, Screen, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import * as Linking from "expo-linking";
import { useLocalSearchParams } from "expo-router";
import * as WebBrowser from "expo-web-browser";
import { LogIn, Plug, Unplug } from "lucide-react-native";
import { useCallback, useEffect, useRef, useState } from "react";
import { View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";

/**
 * Where the API's callback sends the auth session back. Must equal the backend's
 * mobileReturnURL (backend/internal/connectors/connectors.go): both carry this app's scheme.
 */
const RETURN_URL = "scaffoldapp://settings/connections";

const relative = new Intl.RelativeTimeFormat("en-US", { numeric: "auto" });
function ago(iso: string): string {
  const s = Math.round((new Date(iso).getTime() - Date.now()) / 1000);
  const abs = Math.abs(s);
  if (abs < 60) return "just now";
  if (abs < 3600) return relative.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return relative.format(Math.round(s / 3600), "hour");
  return relative.format(Math.round(s / 86_400), "day");
}

/**
 * Settings → Connections on mobile: connected services with their status (the ⋮ menu
 * reconnects, disconnects and, with the agents feature, sets AI agent access for that
 * service) and services to connect. OAuth connects in the system browser
 * (an auth session that returns to this screen); token services and the per-tool
 * switches are on the web.
 */
export function ConnectionsScreen() {
  const theme = useTheme();
  const list = useConnectors();
  const start = useStartConnectorConnect();
  const finish = useFinishConnectorConnect();
  const params = useLocalSearchParams<{ finish?: string; connect_error?: string }>();
  const [disconnecting, setDisconnecting] = useState<Connector | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const disconnect = useDisconnectConnector();
  const handled = useRef(new Set<string>());

  // Android also routes the return link here; iOS hands it to the auth session only.
  const complete = useCallback(
    (secret: string | undefined, error: string | undefined) => {
      const key = secret ?? error;
      if (!key || handled.current.has(key)) return;
      handled.current.add(key);
      if (error) {
        toast.error(connectorConnectErrorMessage(error));
        return;
      }
      if (secret) {
        finish.mutate(secret, {
          onSuccess: (c) => toast.success(`Connected to ${c.name}`),
          onError: (e) => toast.error(errorMessage(e, "Couldn't finish connecting. Try again.")),
        });
      }
    },
    [finish],
  );
  useEffect(() => complete(params.finish, params.connect_error), [params.finish, params.connect_error, complete]);

  const connect = async (c: Connector) => {
    setBusyId(c.id);
    try {
      const res = await start.mutateAsync({ id: c.id, client: "mobile" });
      if (!res.authorize_url) return;
      const out = await WebBrowser.openAuthSessionAsync(res.authorize_url, RETURN_URL);
      if (out.type === "success") {
        const q = Linking.parse(out.url).queryParams ?? {};
        const pick = (v: unknown) => (typeof v === "string" ? v : undefined);
        complete(pick(q.finish), pick(q.connect_error));
      }
    } catch (e) {
      toast.error(errorMessage(e, `Couldn't start connecting to ${c.name}. Try again.`));
    } finally {
      setBusyId(null);
    }
  };

  if (list.isPending) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Skeleton height={theme.tap.base * 2} />
        <Skeleton height={theme.tap.base * 3} />
      </Screen>
    );
  }
  if (list.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <ErrorState title="Couldn't load connections" onRetry={() => void list.refetch()} retrying={list.isFetching} />
      </Screen>
    );
  }
  const items = list.data.items;
  const connected = items.filter((c) => c.connection);
  const available = items.filter((c) => !c.connection && c.available);
  if (items.length === 0) {
    return (
      <Screen edges={["bottom"]} centered>
        <EmptyState icon={Plug} title="No services to connect yet" info="An admin adds services in the admin console." />
      </Screen>
    );
  }
  const status = (c: Connector) => {
    const state = connectorState(c);
    return (
      <Badge size="sm" dot tone={state === "connected" ? "success" : state === "needs_reauth" ? "warning" : "neutral"}>
        {connectorStateLabel(state)}
      </Badge>
    );
  };

  return (
    <Screen edges={["bottom"]} gap={theme.space[6]}>
      {connected.length > 0 ? (
        <ListGroup title="Connected" info="Choose which tools the assistant may use, and whether it may change things, in Settings → Connections on the web.">
          {connected.map((c) => (
            <ListRow
              key={c.id}
              title={c.name}
              chevron={false}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
                  {c.connection?.status === "needs_reauth" ? status(c) : (
                    <Text role="caption" tone="muted">
                      {c.connection?.last_used_at ? `Used ${ago(c.connection.last_used_at)}` : status(c)}
                    </Text>
                  )}
                  <ResourceMenu
                    feature={connectorFeatureKey(c.id)}
                    kind="connectors.connection"
                    {...(c.connection ? { id: c.connection.id } : {})}
                    title={c.name}
                    label={`${c.name} options`}
                    actions={[
                      ...(c.auth_type === "oauth" ? [{ id: "reconnect", label: "Reconnect", icon: LogIn, onSelect: () => void connect(c) }] : []),
                      { id: "disconnect", label: "Disconnect", icon: Unplug, destructive: true, onSelect: () => setDisconnecting(c) },
                    ]}
                  />
                </View>
              }
            />
          ))}
        </ListGroup>
      ) : null}
      <ListGroup title={connected.length > 0 ? "Available" : "Services"} info="Connecting opens the service's sign-in. You choose what it shares there.">
        {available.length === 0 ? (
          <ListRow title="Everything here is connected" chevron={false} />
        ) : (
          available.map((c) =>
            c.auth_type === "oauth" ? (
              <ListRow
                key={c.id}
                title={c.name}
                {...(c.description ? { info: c.description } : {})}
                trailing={busyId === c.id ? <Text tone="muted">Opening…</Text> : <Text tone="primary">Connect</Text>}
                chevron={false}
                disabled={busyId !== null}
                onPress={() => void connect(c)}
              />
            ) : (
              <ListRow key={c.id} title={c.name} info="Connect it on the web: it needs a personal access token you paste there." trailing={<Text tone="muted">On the web</Text>} chevron={false} />
            ),
          )
        )}
      </ListGroup>

      <ConfirmDialog
        open={disconnecting !== null}
        onClose={() => setDisconnecting(null)}
        title={`Disconnect ${disconnecting?.name ?? ""}?`}
        confirmLabel="Disconnect"
        tone="danger"
        onConfirm={async () => {
          const c = disconnecting;
          if (!c?.connection) return;
          const r = await disconnect.mutateAsync(c.connection.id);
          toast.success(connectorDisconnectedMessage(c.name, r));
        }}
        errorText={(e) => errorMessage(e, "Couldn't disconnect. Try again.")}
      >
        <Body>{`The assistant loses access to your ${disconnecting?.name ?? ""} account right away, and the service is asked to revoke the app's access.`}</Body>
      </ConfirmDialog>
    </Screen>
  );
}
