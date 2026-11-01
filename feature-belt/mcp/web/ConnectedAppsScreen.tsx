"use client";

import { errorMessage, mcpEndpointUrl, useMcpConnections, useRevokeMcpConnection, type McpConnection } from "@scaffoldapp/shared";
import { Badge, Card, ConfirmDialog, Divider, EmptyState, ErrorState, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Plug, Unplug } from "lucide-react";
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
 * The server URL to add sits on top.
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
        info="In Claude, ChatGPT, Cursor or Claude Code, add a custom connector (remote MCP server) with this URL, then sign in when it asks."
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
