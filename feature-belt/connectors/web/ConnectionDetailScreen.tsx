"use client";

import {
  connectorDisconnectedMessage,
  connectorFeatureKey,
  connectorState,
  connectorStateLabel,
  errorMessage,
  hasErrorCode,
  useConnectorConnection,
  useConnectorTools,
  useDeleteCustomConnector,
  useDisconnectConnector,
  useRefreshConnectorTools,
  useSetConnectorToolEnabled,
  useSetConnectorWrites,
  useStartConnectorConnect,
  type Connector,
  type ConnectorTool,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, Divider, EmptyState, ErrorState, IconButton, InfoTip, Skeleton, SkeletonGroup, SkeletonRow, Switch, toast } from "@scaffoldapp/ui-web";
import { ChevronLeft, LogIn, Plug, RefreshCw, Trash2, Unplug, Wrench } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Fragment, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Section } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { ConnectorIcon } from "./ConnectorIcon";
import { TokenDialog } from "./TokenDialog";

/**
 * One connection: its state, the changes switch, the server's tools with a switch each,
 * reconnect when the sign-in lapsed, and disconnect.
 */
export function ConnectionDetailScreen({ id }: { id: string }) {
  const q = useConnectorConnection(id);

  if (q.isPending) {
    return (
      <SkeletonGroup className="flex flex-col gap-section">
        <Skeleton className="h-12 w-56" />
        <Skeleton className="h-40" />
        <Skeleton className="h-56" />
      </SkeletonGroup>
    );
  }
  if (q.isError) {
    return hasErrorCode(q.error, "not_found") ? (
      <EmptyState
        icon={<Plug />}
        title="This connection doesn't exist"
        action={
          <Button asChild variant="secondary">
            <Link href="/settings/connections">All connections</Link>
          </Button>
        }
      />
    ) : (
      <ErrorState title="Couldn't load this connection" onRetry={() => void q.refetch()} retrying={q.isFetching} />
    );
  }
  return <Detail connector={q.data} />;
}

function Detail({ connector: c }: { connector: Connector }) {
  const conn = c.connection;
  const router = useRouter();
  const writes = useSetConnectorWrites();
  const disconnect = useDisconnectConnector();
  const removeServer = useDeleteCustomConnector();
  const start = useStartConnectorConnect();
  const [confirming, setConfirming] = useState(false);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [redirecting, setRedirecting] = useState(false);
  if (!conn) return null;
  const state = connectorState(c);
  const writesLocked = c.write_policy === "disabled";

  const reconnect = () => {
    if (c.auth_type === "bearer") {
      setTokenOpen(true);
      return;
    }
    start.mutate(
      { id: c.id, client: "web" },
      {
        onSuccess: (r) => {
          if (r.authorize_url) {
            setRedirecting(true);
            window.location.assign(r.authorize_url);
          }
        },
        onError: (e) => toast.error(errorMessage(e, `Couldn't start connecting to ${c.name}. Try again.`)),
      },
    );
  };

  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-4">
        <Link
          href="/settings/connections"
          className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap"
        >
          <ChevronLeft className="size-4" aria-hidden />
          Connections
        </Link>
        <div className="flex min-h-control-lg items-center gap-3">
          <ConnectorIcon name={c.name} iconUrl={c.icon_url} size="lg" />
          <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
            <span className="truncate">{c.name}</span>
            {c.description ? <InfoTip content={c.description} size="md" /> : null}
          </h1>
          {/* Agent scope is per service: the agents feature adds "AI agent access" for this key. */}
          <ResourceMenu
            feature={connectorFeatureKey(c.id)}
            kind="connectors.connection"
            id={conn.id}
            title={c.name}
            label={`${c.name} options`}
            actions={[
              { id: "reconnect", label: "Reconnect", icon: LogIn, onSelect: reconnect, disabled: start.isPending || redirecting },
              c.custom
                ? { id: "remove", label: "Remove server", icon: Trash2, destructive: true, onSelect: () => setConfirming(true) }
                : { id: "disconnect", label: "Disconnect", icon: Unplug, destructive: true, onSelect: () => setConfirming(true) },
            ]}
          />
        </div>
      </div>

      {state === "needs_reauth" ? (
        <Banner
          tone="warning"
          action={
            <Button size="sm" loading={start.isPending || redirecting} onClick={reconnect}>
              Reconnect
            </Button>
          }
        >
          {conn.last_error || `${c.name} needs you to sign in again.`}
        </Banner>
      ) : state === "unavailable" ? (
        <Banner tone="info">{c.name} is turned off on this server, so the assistant can't use it. You can still disconnect.</Banner>
      ) : null}

      <Section title="Connection">
        <Card padding="none">
          <Row label="Status">
            <Badge size="sm" tone={state === "connected" ? "success" : state === "needs_reauth" ? "warning" : "neutral"} dot>
              {connectorStateLabel(state)}
            </Badge>
          </Row>
          <Divider className="mx-4" />
          <Row label="Connected">{formatDateTime(conn.connected_at)}</Row>
          <Divider className="mx-4" />
          <Row label="Last used">{conn.last_used_at ? formatRelative(conn.last_used_at) : "Not used yet"}</Row>
          <Divider className="mx-4" />
          <Row label="Server">
            <code className="truncate font-mono type-label">{c.server_host}</code>
          </Row>
          {conn.scopes.length > 0 ? (
            <>
              <Divider className="mx-4" />
              <Row label="Access" info="What the service granted when you signed in.">
                <span className="truncate">{conn.scopes.join(", ")}</span>
              </Row>
            </>
          ) : null}
          {conn.last_error && state === "connected" ? (
            <>
              <Divider className="mx-4" />
              <Row label="Last problem">
                <span className="truncate text-warning-strong">{conn.last_error}</span>
              </Row>
            </>
          ) : null}
        </Card>
      </Section>

      <Section title="Changes">
        <Card padding="compact">
          <Switch
            label="Allow changes"
            info={
              writesLocked
                ? `Changes through ${c.name} are turned off on this server.`
                : `Lets the assistant use ${c.name} tools that create, edit or delete things. It asks you to approve each one first.`
            }
            checked={conn.writes_enabled && !writesLocked}
            disabled={writesLocked || writes.isPending}
            onCheckedChange={(v) =>
              writes.mutate(
                { id: conn.id, writesEnabled: v },
                { onError: (e) => toast.error(errorMessage(e, "Couldn't save that. Try again.")) },
              )
            }
          />
        </Card>
      </Section>

      <ToolsSection connector={c} />

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={c.custom ? `Remove ${c.name}?` : `Disconnect ${c.name}?`}
        confirmLabel={c.custom ? "Remove" : "Disconnect"}
        tone="danger"
        onConfirm={async () => {
          if (c.custom) {
            await removeServer.mutateAsync(c.id);
            toast.success("Server removed");
          } else {
            const r = await disconnect.mutateAsync(conn.id);
            toast.success(connectorDisconnectedMessage(c.name, r));
          }
          router.replace("/settings/connections");
        }}
        errorText={(e) => errorMessage(e, "Couldn't disconnect. Try again.")}
      >
        {`The assistant loses access to your ${c.name} account right away, and ${c.name} is asked to revoke the app's access.`}
      </ConfirmDialog>
      <TokenDialog connector={tokenOpen ? c : null} onOpenChange={setTokenOpen} />
    </div>
  );
}

function Row({ label, info, children }: { label: string; info?: string; children: ReactNode }) {
  return (
    <div className="flex min-h-row items-center gap-3 px-4 py-2">
      <span className="flex shrink-0 items-center gap-1 type-body text-fg-muted">
        {label}
        {info ? <InfoTip content={info} /> : null}
      </span>
      <span className="flex min-w-0 flex-1 justify-end type-body text-fg">{children}</span>
    </div>
  );
}

function ToolsSection({ connector: c }: { connector: Connector }) {
  const conn = c.connection;
  const tools = useConnectorTools(conn && conn.status === "active" ? conn.id : null);
  const refresh = useRefreshConnectorTools();
  const toggle = useSetConnectorToolEnabled();
  if (!conn) return null;
  const items = tools.data?.items ?? [];

  return (
    <Section
      title="Tools"
      info={`What ${c.name} offers the assistant. Switch off anything you don't want it to use.`}
      action={
        conn.status === "active" ? (
          <IconButton
            label="Check for new tools"
            tooltip
            size="sm"
            variant="ghost"
            icon={<RefreshCw />}
            disabled={refresh.isPending}
            onClick={() =>
              refresh.mutate(conn.id, {
                onSuccess: () => toast.success("Tool list updated"),
                onError: (e) => toast.error(errorMessage(e, `Couldn't reach ${c.name}. Try again.`)),
              })
            }
          />
        ) : undefined
      }
    >
      <Card padding="none">
        {conn.status !== "active" ? (
          <EmptyState compact icon={<Wrench />} title="Reconnect to see the tools" />
        ) : tools.isPending ? (
          <SkeletonGroup className="flex flex-col">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : tools.isError ? (
          <ErrorState compact title={`Couldn't load ${c.name}'s tools`} info={tools.error.message} onRetry={() => void tools.refetch()} retrying={tools.isFetching} />
        ) : items.length === 0 ? (
          <EmptyState compact icon={<Wrench />} title={`${c.name} offers no tools`} />
        ) : (
          <ul aria-label={`${c.name} tools`}>
            {items.map((t, i) => (
              <Fragment key={t.name}>
                {i > 0 ? <Divider className="mx-4" /> : null}
                <li>
                  <ToolRow
                    tool={t}
                    service={c.name}
                    writesOn={conn.writes_enabled && c.write_policy !== "disabled"}
                    custom={c.custom}
                    onToggle={(enabled) =>
                      toggle.mutate(
                        { id: conn.id, tool: t.name, enabled },
                        { onError: (e) => toast.error(errorMessage(e, "Couldn't save that. Try again.")) },
                      )
                    }
                  />
                </li>
              </Fragment>
            ))}
          </ul>
        )}
      </Card>
    </Section>
  );
}

function ToolRow({ tool: t, service, writesOn, custom, onToggle }: { tool: ConnectorTool; service: string; writesOn: boolean; custom: boolean; onToggle: (enabled: boolean) => void }) {
  const blocked = !t.read_only && !writesOn;
  const info = [
    t.description,
    t.read_only ? null : custom ? `Your own servers don't say which tools only read, so this counts as a change.` : `Changes things in ${service}.`,
    blocked ? "Available when Allow changes is on." : null,
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <div className="flex min-h-row items-center gap-3 px-4 py-2">
      <span className="flex min-w-0 flex-1 items-center gap-2">
        <span className={blocked ? "truncate type-body text-fg-muted" : "truncate type-body text-fg"}>{t.title}</span>
        <Badge size="sm" tone={t.read_only ? "neutral" : "warning"}>
          {t.read_only ? "Reads" : "Changes"}
        </Badge>
        {info ? <InfoTip content={info} /> : null}
      </span>
      <Switch aria-label={`Let the assistant use ${t.title}`} checked={t.enabled && !blocked} disabled={blocked} onCheckedChange={onToggle} />
    </div>
  );
}
