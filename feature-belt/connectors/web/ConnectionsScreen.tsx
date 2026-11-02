"use client";

import {
  connectorConnectErrorMessage,
  connectorPlatformErrorMessage,
  connectorTokenLabel,
  errorMessage,
  sourceProductLabel,
  useActiveOrg,
  useConnectorPlatforms,
  useFinishConnectorConnect,
  useFinishConnectorPlatform,
  useSetConnectorPlatformCapability,
  type ConnectorPlatform,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, ErrorState, ProviderIcon, SkeletonGroup, SkeletonRow, Spinner, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Check, KeyRound, Lock } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { AssistantToolsSection } from "./AssistantTools";
import { PlatformCard, useStartPlatformSignIn } from "./PlatformCard";
import { PlatformTokenDialog } from "./PlatformTokenDialog";

/** An app path to go back to, or null (never another origin). */
function safeReturn(raw: string | null): string | null {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.includes("\\")) return null;
  return raw;
}

/**
 * Settings → Connections, by platform: each platform's connected accounts, which
 * capabilities each powers (grouped by feature), Connect per capability and personal
 * tokens; then the assistant's MCP tools. `?capability=<key>&return_to=<path>` (from a
 * feature's ConnectPrompt or a Reconnect link) lands on that platform with the capability
 * picked and returns afterwards. Sign-ins come back with `?platform_finish=`, `?finish=`
 * (MCP) or `?connect_error=`.
 */
export function ConnectionsScreen() {
  const { org } = useActiveOrg();
  const orgId = org?.id ?? null;
  const platforms = useConnectorPlatforms(orgId);
  const router = useRouter();
  const params = useSearchParams();
  const finishMcp = useFinishConnectorConnect();
  const finishPlatform = useFinishConnectorPlatform();
  const [returnError, setReturnError] = useState<string | null>(null);
  const [tokenFor, setTokenFor] = useState<{ platform: ConnectorPlatform; capability?: string } | null>(null);
  const handled = useRef<string | null>(null);

  const capability = params.get("capability");
  const returnTo = safeReturn(params.get("return_to"));
  const mcpSecret = params.get("finish");
  const platformSecret = params.get("platform_finish");
  const connectError = params.get("connect_error");

  useEffect(() => {
    const key = platformSecret ?? mcpSecret ?? connectError;
    if (!key || handled.current === key) return;
    handled.current = key;
    if (platformSecret) {
      finishPlatform.mutate(
        { secret: platformSecret, orgId },
        {
          onSuccess: (f) => {
            const name = sourceProductLabel(f.connection.provider);
            if (f.capability && !f.capability_enabled) toast.warning(`Connected ${f.connection.account}, but ${name} didn't grant everything asked for. Try again and allow all of it.`);
            else if (f.other_account) toast.success(`Connected ${f.connection.account} as another ${name} account`);
            else toast.success(`Connected ${name} · ${f.connection.account}`);
            const back = safeReturn(f.return_to);
            router.replace(back ?? "/settings/connections");
          },
          onError: (e) => {
            setReturnError(errorMessage(e, "Couldn't finish connecting. Try again."));
            router.replace("/settings/connections");
          },
        },
      );
    } else if (mcpSecret) {
      finishMcp.mutate(mcpSecret, {
        onSuccess: (c) => {
          toast.success(`Connected to ${c.name}`);
          router.replace(c.connection ? `/settings/connections/${c.connection.id}` : "/settings/connections");
        },
        onError: (e) => {
          setReturnError(errorMessage(e, "Couldn't finish connecting. Try again."));
          router.replace("/settings/connections");
        },
      });
    } else if (connectError) {
      setReturnError(connectError === "provider_error" || connectError === "exchange_failed" ? connectorPlatformErrorMessage("The platform", connectError) : connectorConnectErrorMessage(connectError));
      router.replace("/settings/connections");
    }
  }, [platformSecret, mcpSecret, connectError, finishPlatform, finishMcp, router, orgId]);

  const items = platforms.data?.items ?? [];
  const focus = capability ? items.find((p) => p.capabilities.some((c) => c.key === capability)) : undefined;

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Connections"
        info="Connect your accounts on other platforms once. Every feature that works with a platform uses the same connection, as you: it reaches only what your account there can reach."
      />
      {finishPlatform.isPending || finishMcp.isPending ? (
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
      {focus && capability ? (
        <CapabilityPrompt platform={focus} capability={capability} orgId={orgId} returnTo={returnTo} onToken={(p, c) => setTokenFor({ platform: p, ...(c ? { capability: c } : {}) })} />
      ) : null}

      <Section title="Platforms" info="Each platform's accounts and what each one powers here. Turn a capability on and the platform asks you to allow just what it needs.">
        {platforms.isPending ? (
          <Card padding="none">
            <SkeletonGroup label="Loading platforms" className="flex flex-col">
              <SkeletonRow leading />
              <SkeletonRow leading />
              <SkeletonRow leading />
            </SkeletonGroup>
          </Card>
        ) : platforms.isError ? (
          <Card padding="compact">
            <ErrorState compact title="Couldn't load platforms" info={platforms.error.message} onRetry={() => void platforms.refetch()} retrying={platforms.isFetching} />
          </Card>
        ) : (
          <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
            {items.map((p) => (
              <PlatformCard
                key={p.id}
                platform={p}
                orgId={orgId}
                focusCapability={focus?.id === p.id ? capability : null}
                returnTo={returnTo}
                onToken={(pl, c) => setTokenFor({ platform: pl, ...(c ? { capability: c } : {}) })}
              />
            ))}
          </div>
        )}
      </Section>

      <AssistantToolsSection />

      <PlatformTokenDialog
        platform={tokenFor?.platform ?? null}
        capability={tokenFor?.capability}
        orgId={orgId}
        onOpenChange={(o) => !o && setTokenFor(null)}
        onConnected={() => {
          if (returnTo && tokenFor?.capability) router.push(returnTo);
        }}
      />
    </div>
  );
}

/**
 * The deep link's offer: the capability a feature asked for, with the quickest way to
 * turn it on (an existing account, a new sign-in, or a token) and a way back.
 */
function CapabilityPrompt({
  platform: p,
  capability,
  orgId,
  returnTo,
  onToken,
}: {
  platform: ConnectorPlatform;
  capability: string;
  orgId: string | null;
  returnTo: string | null;
  onToken: (p: ConnectorPlatform, capability?: string) => void;
}) {
  const router = useRouter();
  const cap = p.capabilities.find((c) => c.key === capability);
  const signIn = useStartPlatformSignIn(p, orgId, returnTo);
  const setCap = useSetConnectorPlatformCapability();
  if (!cap) return null;
  const onFor = p.connections.find((c) => c.capabilities.some((x) => x.key === capability && x.state === "on"));
  const usable = p.connections.filter((c) => c.status === "active" && c.capabilities.some((x) => x.key === capability && (x.state === "off" || x.state === "needs_grant")));
  const back = () => router.push(returnTo ?? "/settings/connections");
  const blocked = cap.policy === "blocked" || !p.policy.allowed;
  return (
    <Card padding="compact" className="flex flex-col gap-3 border-primary">
      <div className="flex min-h-control items-center gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary-strong">
          <ProviderIcon provider={p.id} size={20} />
        </span>
        <h2 className="line-clamp-2 min-w-0 flex-1 type-title-sm text-fg">{onFor ? cap.label : `Connect ${cap.label}`}</h2>
      </div>
      {onFor ? (
        <p className="flex items-center gap-2 type-body text-fg-muted">
          <Check className="size-4 shrink-0 text-success-strong" aria-hidden />
          <span className="min-w-0 truncate">On for {onFor.account}</span>
        </p>
      ) : null}
      {blocked ? (
        <p className="flex items-center gap-2 type-body text-fg-muted">
          <Lock className="size-4 shrink-0" aria-hidden />
          {cap.blocked_reason ?? "Your workspace doesn't allow this."}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {onFor ? (
          <Button size="sm" leftIcon={<Check />} onClick={back}>
            {returnTo ? "Back" : "Done"}
          </Button>
        ) : blocked ? null : (
          <>
            {usable.map((c) => (
              <Button
                key={c.id}
                size="sm"
                loading={setCap.isPending && setCap.variables?.connectionId === c.id}
                onClick={() =>
                  setCap.mutate(
                    { connectionId: c.id, capability, enabled: true, client: "web", ...(returnTo ? { returnTo } : {}), ...(orgId ? { orgId } : {}) },
                    {
                      onSuccess: (r) => {
                        if (r.authorize_url) window.location.assign(r.authorize_url);
                        else {
                          toast.success(`${cap.label} is on for ${c.account}`);
                          if (returnTo) router.push(returnTo);
                        }
                      },
                      onError: (e) => toast.error(errorMessage(e, "Couldn't turn it on. Try again.")),
                    },
                  )
                }
              >
                Use {c.account}
              </Button>
            ))}
            {p.oauth ? (
              <Button size="sm" variant={usable.length > 0 ? "secondary" : "primary"} loading={signIn.pending} onClick={() => signIn.start({ capability })}>
                {usable.length > 0 || p.connections.length > 0 ? "Another account" : `Connect ${p.name}`}
              </Button>
            ) : null}
            {p.token ? (
              <Button size="sm" variant="ghost" leftIcon={<KeyRound />} onClick={() => onToken(p, capability)}>
                Use {connectorTokenLabel(p.token.label)}
              </Button>
            ) : null}
            {!p.oauth && !p.token ? <span className="type-body text-fg-muted">{p.name} isn't set up on this server yet.</span> : null}
          </>
        )}
        {returnTo && !onFor ? (
          <Button size="sm" variant="ghost" leftIcon={<ArrowLeft />} onClick={back}>
            Back
          </Button>
        ) : null}
      </div>
    </Card>
  );
}
