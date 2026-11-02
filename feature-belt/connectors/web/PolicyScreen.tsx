"use client";

import {
  connectorCapabilitiesByFeature,
  errorMessage,
  orgRoleAtLeast,
  orgsNounA,
  orgsNounTitle,
  useActiveOrg,
  useConnectorPolicy,
  useOrgPickerVisible,
  useResetConnectorPolicy,
  useSetConnectorPolicy,
  type ConnectorPolicyInput,
  type ConnectorPolicyPlatform,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, Divider, EmptyState, ErrorState, InfoTip, ProviderIcon, SkeletonGroup, SkeletonRow, Switch, toast } from "@scaffoldapp/ui-web";
import { Building2, RotateCcw } from "lucide-react";
import Link from "next/link";
import { Screen, Section } from "@/components/shell/PageHeader";
import { OrgSwitcher } from "@/features/orgs";

/**
 * Settings → Connection policy (workspace IT): which platforms members may connect and
 * use in this workspace, single capabilities to deny, and whether capabilities that change
 * data on a platform are allowed. Applies at once to every feature (connections it blocks
 * show "Blocked"). Owners and admins change it; everyone else sees it. Changes are audited.
 */
export function ConnectionPolicyScreen() {
  const { org, orgs, isPending } = useActiveOrg();
  const picker = useOrgPickerVisible();
  if (!isPending && !org) {
    return (
      <Screen title="Connection policy">
        <Card padding="compact">
          <EmptyState
            compact
            icon={<Building2 />}
            title={`Join or create ${orgsNounA()} first`}
            action={
              <Button asChild size="auto">
                <Link href="/settings/orgs">{orgsNounTitle("many")}</Link>
              </Button>
            }
          />
        </Card>
      </Screen>
    );
  }
  return (
    <Screen
      title="Connection policy"
      info={`Which outside platforms members of ${org?.name ?? orgsNounA()} may connect and use here. It applies at once to every feature; people's own permissions on each platform still apply.`}
    >
      {orgs.length > 1 && picker ? <OrgSwitcher className="self-start" /> : null}
      {org ? <PolicyEditor orgId={org.id} canEdit={orgRoleAtLeast(org.role, "admin")} /> : null}
    </Screen>
  );
}

function PolicyEditor({ orgId, canEdit: roleCanEdit }: { orgId: string; canEdit: boolean }) {
  const q = useConnectorPolicy(orgId);
  const set = useSetConnectorPolicy();
  const reset = useResetConnectorPolicy();
  if (q.isPending) {
    return (
      <Card padding="none">
        <SkeletonGroup label="Loading the policy" className="flex flex-col">
          <SkeletonRow leading />
          <SkeletonRow leading />
          <SkeletonRow leading />
        </SkeletonGroup>
      </Card>
    );
  }
  if (q.isError) {
    return (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load the policy" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      </Card>
    );
  }
  const v = q.data;
  const canEdit = v.can_edit && roleCanEdit;
  const save = (provider: string, body: ConnectorPolicyInput) =>
    set.mutate({ orgId, provider, body }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save the policy. Try again.")) });
  const busy = set.isPending || reset.isPending;
  return (
    <>
      <Section title="Default" info="Applies to every platform without its own decision below, including platforms added later.">
        <Card padding="none">
          <div className="flex min-h-tap-lg items-center gap-3 px-4 py-2">
            <span className="min-w-0 flex-1 type-body text-fg">Allow platforms</span>
            <Switch checked={v.default.allowed} disabled={!canEdit || busy} onCheckedChange={(allowed) => save("*", { allowed })} aria-label="Allow platforms by default" />
          </div>
          <Divider className="mx-4" />
          <div className="flex min-h-tap-lg items-center gap-3 px-4 py-2">
            <span className="flex min-w-0 flex-1 items-center gap-2 type-body text-fg">
              Allow changes
              <InfoTip content="Capabilities that create, edit or delete data on a platform (writing to a Drive file, merging a pull request). Reading stays allowed." />
            </span>
            <Switch
              checked={v.default.allow_writes}
              disabled={!canEdit || busy || !v.default.allowed}
              onCheckedChange={(allow_writes) => save("*", { allow_writes })}
              aria-label="Allow changes by default"
            />
          </div>
        </Card>
      </Section>
      <Section title="Platforms" info={canEdit ? undefined : "Only owners and admins change this."}>
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          {v.platforms.map((p) => (
            <PlatformPolicyCard
              key={p.id}
              platform={p}
              canEdit={canEdit}
              busy={busy}
              onSave={(body) => save(p.id, body)}
              onReset={() => reset.mutate({ orgId, provider: p.id }, { onError: (e) => toast.error(errorMessage(e, "Couldn't reset. Try again.")) })}
            />
          ))}
        </div>
      </Section>
    </>
  );
}

function PlatformPolicyCard({
  platform: p,
  canEdit,
  busy,
  onSave,
  onReset,
}: {
  platform: ConnectorPolicyPlatform;
  canEdit: boolean;
  busy: boolean;
  onSave: (body: ConnectorPolicyInput) => void;
  onReset: () => void;
}) {
  const groups = connectorCapabilitiesByFeature(p.capabilities);
  const denied = new Set(p.policy.denied_capabilities);
  return (
    <Card padding="none">
      <div className="flex min-h-tap-lg items-center gap-3 px-4 py-2">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-md border border-border bg-sunken">
          <ProviderIcon provider={p.id} size={20} />
        </span>
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span className="truncate type-body font-semibold text-fg">{p.name}</span>
          {!p.available ? <Badge size="sm">Not set up</Badge> : null}
          <InfoTip
            content={`${p.connections} connected ${p.connections === 1 ? "account" : "accounts"} among members.${p.policy.explicit ? "" : " Follows the default."}`}
          />
        </span>
        {p.policy.explicit && canEdit ? (
          <Button size="sm" variant="ghost" leftIcon={<RotateCcw />} disabled={busy} onClick={onReset}>
            Default
          </Button>
        ) : null}
        <Switch checked={p.policy.allowed} disabled={!canEdit || busy} onCheckedChange={(allowed) => onSave({ allowed })} aria-label={`Allow ${p.name}`} />
      </div>
      {p.policy.allowed ? (
        <>
          <Divider className="mx-4" />
          <div className="flex min-h-tap items-center gap-3 px-4 py-2">
            <span className="flex min-w-0 flex-1 items-center gap-2 type-body text-fg">
              Allow changes
              <InfoTip content={`Capabilities that change data in ${p.name}. Reading stays allowed.`} />
            </span>
            <Switch checked={p.policy.allow_writes} disabled={!canEdit || busy} onCheckedChange={(allow_writes) => onSave({ allow_writes })} aria-label={`Allow changes through ${p.name}`} />
          </div>
          {groups.length > 0 ? (
            <div className="flex flex-col gap-2 px-4 pb-3">
              {groups.map((g) => (
                <div key={g.module} className="flex flex-col gap-1">
                  <h4 className="type-caption font-semibold text-fg-muted">{g.label}</h4>
                  {g.capabilities.map((c) => (
                    <Checkbox
                      key={c.key}
                      checked={!denied.has(c.key) && (!c.write || p.policy.allow_writes)}
                      disabled={!canEdit || busy || (c.write && !p.policy.allow_writes)}
                      label={c.label}
                      info={`${c.write ? "Changes data" : "Reads"}. Asks ${p.name} for: ${c.scopes.join(", ") || "nothing extra"}.`}
                      onCheckedChange={(on) => {
                        const next = new Set(denied);
                        if (on) next.delete(c.key);
                        else next.add(c.key);
                        onSave({ denied_capabilities: [...next] });
                      }}
                    />
                  ))}
                </div>
              ))}
            </div>
          ) : null}
        </>
      ) : null}
    </Card>
  );
}
