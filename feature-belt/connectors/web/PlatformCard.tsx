"use client";

import {
  connectorCapabilitiesByFeature,
  connectorCapabilityState,
  connectorPlatformAvailable,
  connectorPlatformDisconnectedMessage,
  connectorTokenLabel,
  errorMessage,
  useConnectConnectorPlatform,
  useDisconnectConnectorPlatform,
  useSetConnectorPlatformCapability,
  type ConnectorCapabilityState,
  type ConnectorPlatform,
  type ConnectorPlatformCapability,
  type ConnectorPlatformConnection,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Divider, InfoTip, ProviderIcon, Switch, toast, cn } from "@scaffoldapp/ui-web";
import { KeyRound, Lock, LogIn, Plus, Unplug } from "lucide-react";
import { Fragment, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatRelative } from "@/lib/format";

export interface PlatformCardProps {
  platform: ConnectorPlatform;
  orgId: string | null;
  /** Capability to point at (the deep link): its row is highlighted and scrolled into view. */
  focusCapability?: string | null;
  /** Where to come back after a sign-in (the deep link's return_to). */
  returnTo?: string | null;
  onToken: (platform: ConnectorPlatform, capability?: string) => void;
}

/** What a capability asks the platform for, with notes on scopes that grant more than they say. */
export function capabilityInfo(platform: ConnectorPlatform, c: ConnectorPlatformCapability) {
  return (
    <span className="flex flex-col gap-1">
      {c.write ? <span>Can change data in {platform.name}.</span> : <span>Reads from {platform.name}.</span>}
      {c.scopes.length > 0 ? <span>Asks {platform.name} for: {c.scopes.join(", ")}.</span> : null}
      {c.scope_notes.map((n) => (
        <span key={n.scope}>
          {n.scope}: {n.note}.
        </span>
      ))}
      {c.policy === "blocked" && c.blocked_reason ? <span>{c.blocked_reason}</span> : null}
    </span>
  );
}

/** Starts a platform sign-in and follows the authorization URL. */
export function useStartPlatformSignIn(platform: ConnectorPlatform, orgId: string | null, returnTo?: string | null) {
  const connect = useConnectConnectorPlatform();
  const [redirecting, setRedirecting] = useState(false);
  const start = (opts: { capability?: string; connectionId?: string } = {}) =>
    connect.mutate(
      {
        provider: platform.id,
        client: "web",
        ...(opts.capability ? { capability: opts.capability } : {}),
        ...(opts.connectionId ? { connection_id: opts.connectionId } : {}),
        ...(returnTo ? { return_to: returnTo } : {}),
        ...(orgId ? { org_id: orgId } : {}),
      },
      {
        onSuccess: (r) => {
          if (r.authorize_url) {
            setRedirecting(true);
            window.location.assign(r.authorize_url);
          } else if (r.connection) {
            toast.success(`Turned on for ${r.connection.account}`);
          }
        },
        onError: (e) => toast.error(errorMessage(e, `Couldn't start connecting ${platform.name}. Try again.`)),
      },
    );
  return { start, pending: connect.isPending || redirecting, variables: connect.variables };
}

function PlatformStatus({ platform: p }: { platform: ConnectorPlatform }) {
  if (!p.policy.allowed) {
    return (
      <Badge size="sm" tone="neutral" icon={<Lock />}>
        Blocked
      </Badge>
    );
  }
  if (!connectorPlatformAvailable(p)) {
    return (
      <Badge size="sm" tone="neutral">
        Not set up
      </Badge>
    );
  }
  const active = p.connections.filter((c) => c.status === "active").length;
  if (p.connections.some((c) => c.status === "needs_reauth")) {
    return (
      <Badge size="sm" tone="warning" dot>
        Sign in again
      </Badge>
    );
  }
  if (active > 0) {
    return (
      <Badge size="sm" tone="success" dot>
        {active === 1 ? "Connected" : `${active} accounts`}
      </Badge>
    );
  }
  return null;
}

/**
 * One platform in Settings → Connections: its connected accounts with status, which
 * capabilities each account powers (grouped by feature, a switch each), Connect per
 * capability, and personal-token entry where the platform has tokens.
 */
export function PlatformCard({ platform: p, orgId, focusCapability, returnTo, onToken }: PlatformCardProps) {
  const focused = !!focusCapability && p.capabilities.some((c) => c.key === focusCapability);
  const blocked = !p.policy.allowed;
  const available = connectorPlatformAvailable(p);
  const signIn = useStartPlatformSignIn(p, orgId, returnTo);
  const groups = connectorCapabilitiesByFeature(p.capabilities);
  const blockedReason = p.capabilities.find((c) => c.blocked_reason)?.blocked_reason ?? "Your workspace doesn't allow this platform.";

  return (
    <Card padding="none" id={`platform-${p.id}`} className={cn("scroll-mt-24", focused && "ring-2 ring-primary")}>
      <div className="flex min-h-tap-lg items-center gap-3 px-4 py-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-md border border-border bg-sunken">
          <ProviderIcon provider={p.id} size={20} />
        </span>
        <h3 className="flex min-w-0 flex-1 items-center gap-2 type-body font-semibold text-fg">
          <span className="truncate">{p.name}</span>
          {blocked ? (
            <InfoTip content={blockedReason} />
          ) : !available ? (
            <InfoTip content={`Sign-in with ${p.name} isn't set up on this server. An admin adds it in the server's settings.`} />
          ) : p.capabilities.length === 0 ? (
            <InfoTip content={`No feature here uses ${p.name} yet.`} />
          ) : null}
        </h3>
        <PlatformStatus platform={p} />
      </div>

      {p.connections.map((conn) => (
        <Fragment key={conn.id}>
          <Divider />
          <AccountBlock platform={p} conn={conn} orgId={orgId} focusCapability={focusCapability} returnTo={returnTo} onToken={onToken} />
        </Fragment>
      ))}

      {!blocked && available && p.connections.length === 0 && groups.length > 0 ? (
        <>
          <Divider />
          <div className="flex flex-col gap-3 px-4 py-3">
            {groups.map((g) => (
              <div key={g.module} className="flex flex-col gap-1">
                <h4 className="type-caption font-semibold text-fg-muted">{g.label}</h4>
                <ul className="flex flex-col">
                  {g.capabilities.map((c) => (
                    <li
                      key={c.key}
                      className={cn("flex min-h-tap items-center gap-3 rounded-md px-2 -mx-2", c.key === focusCapability && "bg-primary-soft")}
                    >
                      <span className="flex min-w-0 flex-1 items-center gap-2 type-body text-fg">
                        <span className="truncate">{c.label}</span>
                        <InfoTip content={capabilityInfo(p, c)} />
                      </span>
                      {c.policy === "blocked" ? (
                        <Badge size="sm" tone="neutral" icon={<Lock />}>
                          Blocked
                        </Badge>
                      ) : p.oauth ? (
                        <Button
                          size="sm"
                          variant={c.key === focusCapability ? "primary" : "secondary"}
                          loading={signIn.pending && signIn.variables?.capability === c.key}
                          onClick={() => signIn.start({ capability: c.key })}
                        >
                          Connect
                        </Button>
                      ) : (
                        <Button size="sm" variant="secondary" leftIcon={<KeyRound />} onClick={() => onToken(p, c.key)}>
                          Add token
                        </Button>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </>
      ) : null}

      {!blocked && available ? (
        <>
          <Divider />
          <div className="flex flex-wrap items-center gap-2 px-4 py-3">
            {p.oauth ? (
              <Button size="sm" variant="secondary" leftIcon={p.connections.length > 0 ? <Plus /> : <LogIn />} loading={signIn.pending && !signIn.variables?.capability && !signIn.variables?.connection_id} onClick={() => signIn.start()}>
                {p.connections.length > 0 ? "Add another account" : `Connect ${p.name}`}
              </Button>
            ) : null}
            {p.token ? (
              <Button size="sm" variant="ghost" leftIcon={<KeyRound />} onClick={() => onToken(p)}>
                Use {connectorTokenLabel(p.token.label)}
              </Button>
            ) : null}
          </div>
        </>
      ) : null}
    </Card>
  );
}

const STATE_LABEL: Record<ConnectorCapabilityState, string> = { on: "On", off: "Off", needs_grant: "Needs access", blocked: "Blocked" };

function AccountBlock({
  platform: p,
  conn,
  orgId,
  focusCapability,
  returnTo,
  onToken,
}: {
  platform: ConnectorPlatform;
  conn: ConnectorPlatformConnection;
  orgId: string | null;
  focusCapability?: string | null | undefined;
  returnTo?: string | null | undefined;
  onToken: (platform: ConnectorPlatform, capability?: string) => void;
}) {
  const setCap = useSetConnectorPlatformCapability();
  const disconnect = useDisconnectConnectorPlatform();
  const signIn = useStartPlatformSignIn(p, orgId, returnTo);
  const [confirming, setConfirming] = useState(false);
  const groups = connectorCapabilitiesByFeature(p.capabilities);
  const toggle = (c: ConnectorPlatformCapability, enabled: boolean) =>
    setCap.mutate(
      {
        connectionId: conn.id,
        capability: c.key,
        enabled,
        client: "web",
        ...(returnTo ? { returnTo } : {}),
        ...(orgId ? { orgId } : {}),
      },
      {
        onSuccess: (r) => {
          if (r.authorize_url) window.location.assign(r.authorize_url);
        },
        onError: (e) => toast.error(errorMessage(e, `Couldn't change ${c.label}. Try again.`)),
      },
    );
  const status =
    conn.status === "needs_reauth" ? (
      <Badge size="sm" tone="warning" dot>
        Sign in again
      </Badge>
    ) : conn.status === "blocked" ? (
      <Badge size="sm" tone="neutral" icon={<Lock />}>
        Blocked
      </Badge>
    ) : null;
  return (
    <div className="flex flex-col gap-2 px-4 py-3">
      <div className="flex min-h-control items-center gap-2">
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span className="truncate type-body font-medium text-fg">{conn.account}</span>
          {conn.host ? <span className="hidden truncate type-caption text-fg-muted sm:inline">{conn.host}</span> : null}
          {conn.kind === "token" ? (
            <Badge size="sm" tone="neutral">
              Token
            </Badge>
          ) : null}
          <InfoTip
            content={
              <span className="flex flex-col gap-1">
                {conn.host ? <span>On {conn.host}.</span> : null}
                <span>{conn.last_used_at ? `Used ${formatRelative(conn.last_used_at)}.` : "Not used yet."}</span>
                {conn.last_error ? <span>{conn.last_error}</span> : null}
              </span>
            }
          />
        </span>
        {status}
        {conn.status === "needs_reauth" ? (
          conn.kind === "oauth" ? (
            <Button size="sm" variant="secondary" loading={signIn.pending} onClick={() => signIn.start({ connectionId: conn.id })}>
              Reconnect
            </Button>
          ) : (
            <Button size="sm" variant="secondary" onClick={() => onToken(p)}>
              New token
            </Button>
          )
        ) : null}
        <ResourceMenu
          feature={conn.agent_scope}
          kind="connectors.platform_connection"
          id={conn.id}
          title={`${p.name} · ${conn.account}`}
          label={`${p.name} · ${conn.account} options`}
          actions={[
            conn.kind === "oauth" && p.oauth
              ? { id: "reconnect", label: "Reconnect", icon: LogIn, onSelect: () => signIn.start({ connectionId: conn.id }), disabled: signIn.pending }
              : { id: "token", label: "Replace token", icon: KeyRound, onSelect: () => onToken(p), disabled: !p.token },
            { id: "disconnect", label: "Disconnect", icon: Unplug, destructive: true, onSelect: () => setConfirming(true) },
          ]}
        />
      </div>
      {groups.length > 0 && conn.status !== "blocked" ? (
        <div className="flex flex-col gap-2">
          {groups.map((g) => (
            <div key={g.module} className="flex flex-col">
              <h4 className="type-caption font-semibold text-fg-muted">{g.label}</h4>
              <ul className="flex flex-col">
                {g.capabilities.map((c) => {
                  const state = connectorCapabilityState(conn, c.key);
                  const busy = setCap.isPending && setCap.variables?.capability === c.key;
                  return (
                    <li key={c.key} className={cn("-mx-2 flex min-h-tap items-center gap-3 rounded-md px-2", c.key === focusCapability && "bg-primary-soft")}>
                      <span className="flex min-w-0 flex-1 items-center gap-2 type-body text-fg">
                        <span className="truncate">{c.label}</span>
                        <InfoTip content={capabilityInfo(p, c)} />
                      </span>
                      {state === "needs_grant" ? (
                        <Button size="sm" variant="secondary" loading={busy} onClick={() => toggle(c, true)}>
                          Grant access
                        </Button>
                      ) : state === "blocked" ? (
                        <Lock className="size-4 shrink-0 text-fg-subtle" aria-label="Blocked by your workspace" />
                      ) : null}
                      <Switch
                        checked={state === "on" || state === "needs_grant"}
                        disabled={state === "blocked" || busy || conn.status !== "active"}
                        onCheckedChange={(v) => toggle(c, v)}
                        aria-label={`${c.label} for ${conn.account}: ${STATE_LABEL[state]}`}
                      />
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </div>
      ) : null}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Disconnect ${conn.account}?`}
        confirmLabel="Disconnect"
        tone="danger"
        onConfirm={async () => {
          const r = await disconnect.mutateAsync(conn.id);
          toast.success(connectorPlatformDisconnectedMessage(p.name, conn.account, r));
          setConfirming(false);
        }}
        errorText={(e) => errorMessage(e, "Couldn't disconnect. Try again.")}
      >
        Every feature using this {p.name} account stops showing its content.
        {p.revocable && conn.kind === "oauth" ? ` ${p.name} is asked to revoke this app's access.` : ""}
      </ConfirmDialog>
    </div>
  );
}
