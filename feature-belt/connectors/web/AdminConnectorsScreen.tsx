"use client";

import { useAdminConnectors, useMe, type AdminConnector } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState } from "@scaffoldapp/ui-web";
import { Plug, Plus } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { ConnectorFormDialog } from "./ConnectorFormDialog";
import { ConnectorIcon } from "./ConnectorIcon";
import { PlatformSetupSection } from "./PlatformSetupCard";

const POLICY_LABEL: Record<AdminConnector["write_policy"], string> = { disabled: "Never", off: "Off by default", on: "On by default" };

/**
 * Admin → Connectors: the services users may connect. Support reads; admins add, edit and
 * delete (each change is recorded in the audit log).
 */
export function AdminConnectorsScreen() {
  const me = useMe();
  const list = useAdminConnectors();
  const [editing, setEditing] = useState<AdminConnector | "new" | null>(null);
  const rows = list.data?.items ?? [];
  const canEdit = me.data?.admin_role === "admin";

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Connectors"
        info="Platforms people connect once for every feature, and MCP servers whose tools the assistant can use. Each person reaches only what their own account can reach."
      />
      <PlatformSetupSection />
      <Section
        title="Assistant tools"
        info="MCP servers people can connect with their own account; the assistant uses their tools as that person."
        action={
          canEdit ? (
            <Button size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
              Add connector
            </Button>
          ) : undefined
        }
      >
      <Card padding="none">
        {list.isPending ? (
          <TableSkeleton rows={4} cols={5} />
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load connectors" onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<Plug />}
            title="No connectors yet"
            action={
              canEdit ? (
                <Button size="sm" variant="secondary" leftIcon={<Plus />} onClick={() => setEditing("new")}>
                  Add connector
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <Table label="Connectors" className="min-w-160">
              <THead>
                <TH>Connector</TH>
                <TH>Server</TH>
                <TH>Sign-in</TH>
                <TH info="Whether tools that change things can be offered, and the switch's starting value for new connections.">Changes</TH>
                <TH align="right" info="People connected; how many need to sign in again.">Connected</TH>
                <TH align="right">
                  <span className="sr-only">Actions</span>
                </TH>
              </THead>
              <tbody>
                {rows.map((c) => (
                  <TR key={c.id} {...(canEdit ? { onActivate: () => setEditing(c) } : {})}>
                    <TD>
                      <span className="flex items-center gap-3">
                        <ConnectorIcon name={c.name} iconUrl={c.icon_url} size="sm" />
                        <span className="flex min-w-0 flex-col">
                          <span className="flex items-center gap-2 font-semibold">
                            {c.name}
                            {!c.enabled ? (
                              <Badge size="sm" tone="neutral">
                                Off
                              </Badge>
                            ) : null}
                          </span>
                          <code className="font-mono type-caption text-fg-muted">{c.slug}</code>
                        </span>
                      </span>
                    </TD>
                    <TD className="max-w-56 truncate text-fg-muted">{hostOf(c.server_url)}</TD>
                    <TD>
                      <Badge size="sm" tone="neutral">
                        {c.auth_type === "oauth" ? "OAuth" : "Token"}
                      </Badge>
                    </TD>
                    <TD className="whitespace-nowrap">{POLICY_LABEL[c.write_policy]}</TD>
                    <TD align="right" className="whitespace-nowrap tabular-nums">
                      {c.connections}
                      {c.needs_reauth > 0 ? <span className="ml-2 text-warning-strong">{c.needs_reauth} to reconnect</span> : null}
                    </TD>
                    <TD align="right">
                      {canEdit ? (
                        <Button size="sm" variant="ghost" onClick={() => setEditing(c)}>
                          Edit
                        </Button>
                      ) : null}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
            <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
          </>
        )}
      </Card>
      </Section>
      <ConnectorFormDialog connector={editing === "new" ? null : editing} open={editing !== null} onOpenChange={(o) => !o && setEditing(null)} />
    </div>
  );
}

function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
