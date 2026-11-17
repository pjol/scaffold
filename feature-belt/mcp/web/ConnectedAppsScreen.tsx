"use client";

import {
  errorMessage,
  mcpEndpointUrl,
  useMcpAAuthLinks,
  useMcpConnections,
  useRevokeMcpConnection,
  useUnlinkMcpAAuth,
  type McpAAuthLink,
  type McpConnection,
} from "@scaffoldapp/shared";
import { Badge, Card, ConfirmDialog, Divider, EmptyState, ErrorState, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Plug, ServerCog, Unplug } from "lucide-react";
import { Fragment, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { APP_NAME } from "@/lib/app";
import { API_URL } from "@/lib/env";
import { formatDate, formatRelative } from "@/lib/format";
import { CopyField } from "./CopyField";

/**
 * Settings → Connected apps: agents connected by signing in over OAuth (Claude, ChatGPT,
 * Cursor, Claude Code). One row per app with its access, when it was connected and last
 * used; the row's ⋮ menu (ResourceMenu, feature "mcp", kind "mcp.client") holds Disconnect,
 * which ends every token it holds, and whatever other features add there (agent access).
 * Below, person servers linked over AAuth (kind "mcp.aauth_link", with Unlink). The server
 * URL to add sits on top.
 */
export function ConnectedAppsScreen() {
  const apps = useMcpConnections();
  const revoke = useRevokeMcpConnection();
  const [revoking, setRevoking] = useState<McpConnection | null>(null);
  const rows = apps.data?.items ?? [];
  const endpoint = mcpEndpointUrl(API_URL);

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Connected apps"
        info={`Apps you connected by signing in. Each acts as you and sees only what you can see in ${APP_NAME}.`}
      />
      <Section
        title="Connect an app"
        info="In Claude, ChatGPT, Cursor or Claude Code, add a custom connector (remote MCP server) with this URL, then sign in when it asks. An agent that signs its requests with AAuth uses the same URL and asks you to link its person server."
      >
        <CopyField label="Server URL" value={endpoint} className="max-w-form" />
      </Section>
      <Section title="Apps">
        <Card padding="none">
          {apps.isPending ? (
            <SkeletonGroup className="flex flex-col">
              <SkeletonRow />
              <SkeletonRow />
            </SkeletonGroup>
          ) : apps.isError ? (
            <ErrorState compact title="Couldn't load your connected apps" onRetry={() => void apps.refetch()} retrying={apps.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Plug />} title="No connected apps" />
          ) : (
            <ul>
              {rows.map((a, i) => (
                <Fragment key={a.id}>
                  {i > 0 ? <Divider className="mx-4" /> : null}
                  <li className="flex items-center gap-3 px-4 py-3">
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate type-body font-semibold text-fg">{a.name || "Unnamed app"}</span>
                        <Badge size="sm" tone={a.scopes.includes("write") ? "warning" : "neutral"}>
                          {a.scopes.includes("write") ? "Read and write" : "Read only"}
                        </Badge>
                      </div>
                      <span className="flex flex-wrap gap-x-3 type-caption text-fg-muted">
                        {a.host ? <span>{a.host}</span> : null}
                        <span>Connected {formatDate(a.connected_at)}</span>
                        <span>{a.last_used_at ? `Used ${formatRelative(a.last_used_at)}` : "Never used"}</span>
                        {a.grants > 1 ? <span>{a.grants} sign-ins</span> : null}
                      </span>
                    </div>
                    <ResourceMenu
                      feature="mcp"
                      kind="mcp.client"
                      id={a.id}
                      title={a.name || "Unnamed app"}
                      label={`More actions for ${a.name || "this app"}`}
                      actions={[{ id: "disconnect", label: "Disconnect", icon: Unplug, destructive: true, onSelect: () => setRevoking(a) }]}
                    />
                  </li>
                </Fragment>
              ))}
              <LoadMore hasNextPage={apps.hasNextPage} isFetchingNextPage={apps.isFetchingNextPage} fetchNextPage={apps.fetchNextPage} />
            </ul>
          )}
        </Card>
      </Section>
      <PersonServers />
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={`Disconnect ${revoking?.name || "this app"}?`}
        confirmLabel="Disconnect"
        tone="danger"
        onConfirm={async () => {
          if (!revoking) return;
          await revoke.mutateAsync(revoking.id);
          toast.success("App disconnected");
          setRevoking(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't disconnect the app. Try again.")}
      >
        It stops working right away, on every device. To use it again, connect it and sign in again.
      </ConfirmDialog>
    </div>
  );
}

/** Person servers linked over AAuth: their agents can ask for access as the person. */
function PersonServers() {
  const links = useMcpAAuthLinks();
  const unlink = useUnlinkMcpAAuth();
  const [unlinking, setUnlinking] = useState<McpAAuthLink | null>(null);
  const rows = links.data?.items ?? [];
  const nameOf = (l: McpAAuthLink) => l.person_server.name || l.person_server.host;

  return (
    <Section
      title="Person servers"
      info={`Linked over AAuth. Agents acting for you through a linked person server can ask for access to ${APP_NAME} as you; each needs a grant from that server first.`}
    >
      <Card padding="none">
        {links.isPending ? (
          <SkeletonGroup className="flex flex-col">
            <SkeletonRow />
          </SkeletonGroup>
        ) : links.isError ? (
          <ErrorState compact title="Couldn't load your person servers" onRetry={() => void links.refetch()} retrying={links.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<ServerCog />} title="No linked person servers" />
        ) : (
          <ul>
            {rows.map((l, i) => (
              <Fragment key={l.id}>
                {i > 0 ? <Divider className="mx-4" /> : null}
                <li className="flex items-center gap-3 px-4 py-3">
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <span className="truncate type-body font-semibold text-fg">{nameOf(l)}</span>
                    <span className="flex flex-wrap gap-x-3 type-caption text-fg-muted">
                      {l.person_server.name ? <span>{l.person_server.host}</span> : null}
                      {l.email ? <span>{l.email}</span> : null}
                      <span>Linked {formatDate(l.linked_at)}</span>
                      <span>{l.last_used_at ? `Used ${formatRelative(l.last_used_at)}` : "Never used"}</span>
                      {l.agents > 0 ? <span>{l.agents === 1 ? "1 agent" : `${l.agents} agents`}</span> : null}
                    </span>
                  </div>
                  <ResourceMenu
                    feature="mcp"
                    kind="mcp.aauth_link"
                    id={l.id}
                    title={nameOf(l)}
                    label={`More actions for ${nameOf(l)}`}
                    actions={[{ id: "unlink", label: "Unlink", icon: Unplug, destructive: true, onSelect: () => setUnlinking(l) }]}
                  />
                </li>
              </Fragment>
            ))}
            <LoadMore hasNextPage={links.hasNextPage} isFetchingNextPage={links.isFetchingNextPage} fetchNextPage={links.fetchNextPage} />
          </ul>
        )}
      </Card>
      <ConfirmDialog
        open={unlinking !== null}
        onOpenChange={(o) => !o && setUnlinking(null)}
        title={`Unlink ${unlinking ? nameOf(unlinking) : "this person server"}?`}
        confirmLabel="Unlink"
        tone="danger"
        onConfirm={async () => {
          if (!unlinking) return;
          await unlink.mutateAsync(unlinking.id);
          toast.success("Person server unlinked");
          setUnlinking(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't unlink. Try again.")}
      >
        Its agents stop acting as you right away. An agent that asks again sends you to link it again.
      </ConfirmDialog>
    </Section>
  );
}
