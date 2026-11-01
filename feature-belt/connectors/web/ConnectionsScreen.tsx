"use client";

import {
  connectorConnectErrorMessage,
  connectorState,
  connectorStateLabel,
  errorMessage,
  useConnectors,
  useDeleteCustomConnector,
  useFinishConnectorConnect,
  useStartConnectorConnect,
  type Connector,
} from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, Divider, EmptyState, ErrorState, IconButton, ListRow, SkeletonGroup, SkeletonRow, Spinner, toast } from "@scaffoldapp/ui-web";
import { Plug, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Fragment, useEffect, useRef, useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatRelative } from "@/lib/format";
import { AddServerDialog } from "./AddServerDialog";
import { ConnectorIcon } from "./ConnectorIcon";
import { TokenDialog } from "./TokenDialog";

/**
 * Settings → Connections: services the user connects with their own account so the
 * assistant can use them as that person. Connected services first (each opens its
 * settings), then what is available to connect. The OAuth return lands here with
 * `?finish=` (completed by this signed-in session) or `?connect_error=`.
 */
export function ConnectionsScreen() {
  const list = useConnectors();
  const router = useRouter();
  const params = useSearchParams();
  const finish = useFinishConnectorConnect();
  const [returnError, setReturnError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [tokenFor, setTokenFor] = useState<Connector | null>(null);
  const [removing, setRemoving] = useState<Connector | null>(null);
  const removeCustom = useDeleteCustomConnector();
  const handled = useRef<string | null>(null);

  // Back from the provider: finish with this session, then drop the secret from the URL.
  const secret = params.get("finish");
  const connectError = params.get("connect_error");
  useEffect(() => {
    if (secret && handled.current !== secret) {
      handled.current = secret;
      finish.mutate(secret, {
        onSuccess: (c) => {
          toast.success(`Connected to ${c.name}`);
          router.replace(c.connection ? `/settings/connections/${c.connection.id}` : "/settings/connections");
        },
        onError: (e) => {
          setReturnError(errorMessage(e, "Couldn't finish connecting. Try again."));
          router.replace("/settings/connections");
        },
      });
    } else if (connectError && handled.current !== connectError) {
      handled.current = connectError;
      setReturnError(connectorConnectErrorMessage(connectError));
      router.replace("/settings/connections");
    }
  }, [secret, connectError, finish, router]);

  const items = list.data?.items ?? [];
  const connected = items.filter((c) => c.connection);
  const available = items.filter((c) => !c.connection && c.available);
  const allowCustom = list.data?.allow_custom ?? false;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Connections"
        info="Connect services you use so the assistant can work with them as you. It reaches only what your account there can reach, and asks before changing anything."
        action={
          allowCustom ? (
            <Button size="sm" variant="secondary" leftIcon={<Plus />} onClick={() => setAdding(true)}>
              Add a server
            </Button>
          ) : undefined
        }
      />
      {finish.isPending ? (
        <Banner tone="info" icon={<Spinner size={18} label="Finishing" />}>
          Finishing the connection…
        </Banner>
      ) : null}
      {returnError ? (
        <Banner
          tone="danger"
          action={
            <Button size="sm" variant="ghost" onClick={() => setReturnError(null)}>
              Dismiss
            </Button>
          }
        >
          {returnError}
        </Banner>
      ) : null}

      {list.isPending ? (
        <Card padding="none">
          <SkeletonGroup className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState title="Couldn't load connections" onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={<Plug />}
          title="No services to connect yet"
          info="An admin adds services in the admin console."
          action={
            allowCustom ? (
              <Button variant="secondary" leftIcon={<Plus />} onClick={() => setAdding(true)}>
                Add a server
              </Button>
            ) : undefined
          }
        />
      ) : (
        <>
          {connected.length > 0 ? (
            <Section title="Connected">
              <Card padding="none">
                <ul>
                  {connected.map((c, i) => (
                    <Fragment key={c.id}>
                      {i > 0 ? <Divider className="mx-4" /> : null}
                      <li>
                        <ConnectedRow connector={c} />
                      </li>
                    </Fragment>
                  ))}
                </ul>
              </Card>
            </Section>
          ) : null}
          <Section title={connected.length > 0 ? "Available" : "Services"} info="Connecting opens the service's sign-in. You choose what it shares there.">
            <Card padding="none">
              {available.length === 0 ? (
                <EmptyState compact icon={<Plug />} title="Everything here is connected" />
              ) : (
                <ul>
                  {available.map((c, i) => (
                    <Fragment key={c.id}>
                      {i > 0 ? <Divider className="mx-4" /> : null}
                      <li>
                        <AvailableRow connector={c} onToken={() => setTokenFor(c)} onRemove={c.custom ? () => setRemoving(c) : undefined} />
                      </li>
                    </Fragment>
                  ))}
                </ul>
              )}
            </Card>
          </Section>
        </>
      )}

      <AddServerDialog
        open={adding}
        onOpenChange={setAdding}
        onCreated={(c) => {
          if (c.auth_type === "bearer") setTokenFor(c);
        }}
      />
      <TokenDialog connector={tokenFor} onOpenChange={(o) => !o && setTokenFor(null)} />
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.name ?? "this server"}?`}
        confirmLabel="Remove"
        tone="danger"
        onConfirm={async () => {
          if (!removing) return;
          await removeCustom.mutateAsync(removing.id);
          toast.success("Server removed");
          setRemoving(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the server. Try again.")}
      >
        It disappears from your list. You can add it again later.
      </ConfirmDialog>
    </div>
  );
}

function StateBadge({ connector }: { connector: Connector }) {
  const state = connectorState(connector);
  const tone = state === "connected" ? "success" : state === "needs_reauth" ? "warning" : "neutral";
  return (
    <Badge size="sm" tone={tone} dot>
      {connectorStateLabel(state)}
    </Badge>
  );
}

function ConnectedRow({ connector: c }: { connector: Connector }) {
  const conn = c.connection;
  if (!conn) return null;
  return (
    <ListRow
      asChild
      interactive
      leading={<ConnectorIcon name={c.name} iconUrl={c.icon_url} />}
      title={<span className="truncate">{c.name}</span>}
      trailing={
        <span className="flex items-center gap-3">
          <span className="hidden type-caption text-fg-muted sm:inline">{conn.last_used_at ? `Used ${formatRelative(conn.last_used_at)}` : "Not used yet"}</span>
          <StateBadge connector={c} />
        </span>
      }
    >
      <Link href={`/settings/connections/${conn.id}`} aria-label={`${c.name}: ${connectorStateLabel(connectorState(c))}`} />
    </ListRow>
  );
}

function AvailableRow({ connector: c, onToken, onRemove }: { connector: Connector; onToken: () => void; onRemove?: (() => void) | undefined }) {
  const start = useStartConnectorConnect();
  const [redirecting, setRedirecting] = useState(false);
  const connect = () => {
    if (c.auth_type === "bearer") {
      onToken();
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
    <ListRow
      leading={<ConnectorIcon name={c.name} iconUrl={c.icon_url} />}
      title={
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate">{c.name}</span>
          {c.custom ? (
            <Badge size="sm" tone="neutral">
              Your server
            </Badge>
          ) : null}
        </span>
      }
      info={c.description || c.server_host || undefined}
      trailing={
        <span className="flex items-center gap-2">
          {onRemove ? <IconButton label={`Remove ${c.name}`} tooltip size="sm" variant="ghost" icon={<Trash2 />} onClick={onRemove} /> : null}
          <Button size="sm" variant="secondary" loading={start.isPending || redirecting} onClick={connect}>
            Connect
          </Button>
        </span>
      }
    />
  );
}
