"use client";

import { useAdminConnectorPlatforms } from "@scaffoldapp/shared";
import { Badge, Card, ErrorState, IconButton, InfoTip, ProviderIcon, toast } from "@scaffoldapp/ui-web";
import { Copy } from "lucide-react";
import { Section } from "@/components/shell/PageHeader";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";

/**
 * Admin → Connectors → Platforms: each platform's setup on this server (sign-in client id
 * and secret, personal tokens, webhooks) and how many accounts people connected. Read-only:
 * setup lives in the server's environment.
 */
export function PlatformSetupSection() {
  const q = useAdminConnectorPlatforms();
  const redirect = q.data?.items[0]?.redirect_uri;
  return (
    <Section
      title="Platforms"
      info="Each platform people connect once and use from every feature. Sign-in needs an OAuth app registered with the platform; set its client ID and secret in the server's environment."
      action={
        redirect ? (
          <span className="flex min-w-0 items-center gap-1">
            <code className="hidden max-w-80 truncate font-mono type-caption text-fg-muted md:inline">{redirect}</code>
            <IconButton
              label="Copy the redirect URL"
              tooltip
              size="sm"
              variant="ghost"
              icon={<Copy />}
              onClick={() => {
                void navigator.clipboard?.writeText(redirect).then(() => toast.success("Redirect URL copied"));
              }}
            />
          </span>
        ) : undefined
      }
    >
      <Card padding="none">
        {q.isPending ? (
          <TableSkeleton rows={4} cols={5} />
        ) : q.isError ? (
          <ErrorState compact title="Couldn't load platform setup" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        ) : (
          <Table label="Platforms" className="min-w-160">
            <THead>
              <TH>Platform</TH>
              <TH info="Register the redirect URL (copy it above) with the platform's OAuth app.">Sign-in</TH>
              <TH>Tokens</TH>
              <TH info="Optional: platforms can notify features of changes sooner than polling.">Webhooks</TH>
              <TH align="right">Accounts</TH>
            </THead>
            <tbody>
              {q.data.items.map((p) => (
                <TR key={p.id}>
                  <TD>
                    <span className="flex items-center gap-3">
                      <ProviderIcon provider={p.id} size={18} />
                      <span className="font-semibold">{p.name}</span>
                      {p.base_url ? <span className="hidden truncate type-caption text-fg-muted lg:inline">{p.base_url}</span> : null}
                      <InfoTip
                        content={
                          <span className="flex flex-col gap-1">
                            <span>{p.capabilities} capabilities from installed features.</span>
                            {p.scopes.length > 0 ? <span>Scopes they may ask for: {p.scopes.join(", ")}.</span> : null}
                            <span>{p.revocation ? "Disconnecting revokes access at the platform." : "The platform can't revoke access from here; people remove the app there."}</span>
                            {p.dev_override ? <span>Development override: {p.dev_override}</span> : null}
                            {p.register_url ? <span>Register the app at {p.register_url}</span> : null}
                          </span>
                        }
                      />
                    </span>
                  </TD>
                  <TD>
                    {p.oauth ? (
                      <Badge size="sm" tone="success" dot>
                        Set up
                      </Badge>
                    ) : (
                      <span className="flex items-center gap-2">
                        <Badge size="sm" tone="neutral">
                          Not set up
                        </Badge>
                        <code className="hidden font-mono type-caption text-fg-muted xl:inline">{p.missing.join(", ")}</code>
                      </span>
                    )}
                  </TD>
                  <TD className="text-fg-muted">{p.tokens ? "Yes" : "No"}</TD>
                  <TD className="text-fg-muted">{p.webhook_path ? (p.webhook_ready ? "Ready" : "Secret not set") : "None"}</TD>
                  <TD align="right" className="tabular-nums">
                    {p.connections}
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </Section>
  );
}
