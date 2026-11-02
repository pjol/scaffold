"use client";

import {
  connectorState,
  connectorStateLabel,
  errorMessage,
  useConnectors,
  useDeleteCustomConnector,
  useStartConnectorConnect,
  type Connector,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Divider, EmptyState, ErrorState, IconButton, ListRow, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Plug, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { Fragment, useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { formatRelative } from "@/lib/format";
import { AddServerDialog } from "./AddServerDialog";
import { ConnectorIcon } from "./ConnectorIcon";
import { TokenDialog } from "./TokenDialog";

/**
 * Settings → Connections → Assistant tools: MCP servers people connect with their own
 * account so the assistant can use their tools as that person. Connected servers first
 * (each opens its settings), then what is available to connect.
 */
export function AssistantToolsSection() {
  const list = useConnectors();
  const [adding, setAdding] = useState(false);
  const [tokenFor, setTokenFor] = useState<Connector | null>(null);
  const [removing, setRemoving] = useState<Connector | null>(null);
  const removeCustom = useDeleteCustomConnector();

  const items = list.data?.items ?? [];
  const connected = items.filter((c) => c.connection);
  const available = items.filter((c) => !c.connection && c.available);
  const allowCustom = list.data?.allow_custom ?? false;

  return (
    <Section
      title="Assistant tools"
      info="Services whose tools the assistant can use as you, through their MCP servers. Connecting opens the service's sign-in; the assistant asks before changing anything."
      action={
        allowCustom ? (
          <Button size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => setAdding(true)}>
            Add a server
          </Button>
        ) : undefined
      }
    >
      {list.isPending ? (
        <Card padding="none">
          <SkeletonGroup className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <Card padding="compact">
          <ErrorState compact title="Couldn't load assistant tools" onRetry={() => void list.refetch()} retrying={list.isFetching} />
        </Card>
      ) : items.length === 0 ? (
        <Card padding="compact">
          <EmptyState compact icon={<Plug />} title="No assistant tools to connect yet" info="An admin adds MCP servers in the admin console." />
        </Card>
      ) : (
        <Card padding="none">
          <ul>
            {[...connected, ...available].map((c, i) => (
              <Fragment key={c.id}>
                {i > 0 ? <Divider className="mx-4" /> : null}
                <li>{c.connection ? <ConnectedRow connector={c} /> : <AvailableRow connector={c} onToken={() => setTokenFor(c)} onRemove={c.custom ? () => setRemoving(c) : undefined} />}</li>
              </Fragment>
            ))}
          </ul>
        </Card>
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
    </Section>
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
