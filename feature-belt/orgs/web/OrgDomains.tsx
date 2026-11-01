"use client";

import {
  errorMessage,
  hasErrorCode,
  orgsNoun,
  orgsNounTitle,
  useAddOrgDomain,
  useMe,
  useOrgDomains,
  useRemoveOrgDomain,
  useUpdateOrgDomain,
  useVerifyOrgDomain,
  type Org,
  type OrgDomain,
  type OrgDomainPolicy,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, IconButton, Input, ListRow, Menu, MenuItem, Select, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { AtSign, Copy, MoreHorizontal, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { DOMAIN_POLICY_OPTIONS, DOMAIN_STATUS_LABEL, DOMAIN_STATUS_TONE } from "./labels";

async function copy(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`${what} copied`);
  } catch {
    toast.error("Couldn't copy. Select the text and copy it.");
  }
}

function AddDomainForm({ org }: { org: Org }) {
  const add = useAddOrgDomain(org.id);
  const [domain, setDomain] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const value = domain.trim().toLowerCase();
    if (!value.includes(".")) return setError("Enter a domain like example.com.");
    setError(null);
    add.mutate(
      { domain: value },
      {
        onSuccess: (d) => {
          setDomain("");
          toast.success(d.status === "verified" ? `${d.domain} verified with your email` : `${d.domain} added. Add the DNS record to verify it.`);
        },
        onError: (err) => (hasErrorCode(err, "conflict") || hasErrorCode(err, "invalid_request") ? setError(errorMessage(err)) : toast.error(errorMessage(err, "Couldn't add the domain. Try again."))),
      },
    );
  };
  return (
    <form className="flex max-w-form flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submit} noValidate>
      <Field label="Domain" error={error ?? undefined} className="flex-1" info={`Your ${orgsNoun()}'s own email domain. Public providers such as gmail.com can't be added.`}>
        <Input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" autoComplete="off" autoCapitalize="none" spellCheck={false} />
      </Field>
      <Button type="submit" size="auto" leftIcon={<Plus />} loading={add.isPending}>
        Add domain
      </Button>
    </form>
  );
}

function verifiedInfo(d: OrgDomain): string {
  const by = d.verified_by_name ? ` by ${d.verified_by_name}` : "";
  const how = d.verify_method === "dns" ? "DNS record" : "email address";
  const base = `Verified${by} with ${d.verify_method === "email" ? "their" : "a"} ${how} on ${d.verified_at ? formatDate(d.verified_at) : "an earlier date"}. People join as members.`;
  return needsDnsForAutoJoin(d) ? `${base} Until the DNS record below is verified, people join from "${orgsNounTitle("many")} you can join" rather than on opening the app.` : base;
}

/** An email proves one mailbox, not the domain: joining people on opening the app needs the DNS record. */
function needsDnsForAutoJoin(d: OrgDomain): boolean {
  return d.status === "verified" && d.verify_method === "email" && d.policy === "auto_join";
}

/** The TXT record to publish, with copy buttons, and the check button. */
function DnsSteps({ org, d }: { org: Org; d: OrgDomain }) {
  const verify = useVerifyOrgDomain(org.id);
  const check = () =>
    verify.mutate(d.id, {
      onSuccess: (v) => toast.success(`${v.domain} verified`),
      onError: (e) => (hasErrorCode(e, "verification_failed") ? undefined : toast.error(errorMessage(e, "Couldn't check the domain. Try again."))),
    });
  return (
    <div className="flex flex-col gap-4 px-4 pb-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="TXT record name" info="Add a TXT record with this name and value at your DNS provider, then check.">
          <Input value={d.txt_name} readOnly onFocus={(e) => e.currentTarget.select()} trailing={<IconButton label="Copy record name" icon={<Copy />} size="sm" variant="ghost" onClick={() => void copy(d.txt_name, "Name")} />} />
        </Field>
        <Field label="TXT record value">
          <Input value={d.txt_value} readOnly onFocus={(e) => e.currentTarget.select()} trailing={<IconButton label="Copy record value" icon={<Copy />} size="sm" variant="ghost" onClick={() => void copy(d.txt_value, "Value")} />} />
        </Field>
      </div>
      <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
        <Button variant="secondary" size="sm" leftIcon={<RefreshCw />} loading={verify.isPending} onClick={check}>
          Check now
        </Button>
        {d.last_check_error && (
          <p role="status" className="min-w-0 flex-1 type-label text-warning-strong">
            {d.last_check_error}
          </p>
        )}
      </div>
    </div>
  );
}

function DomainItem({ org, d, onRemove }: { org: Org; d: OrgDomain; onRemove: (d: OrgDomain) => void }) {
  const update = useUpdateOrgDomain(org.id);
  const policy = (p: string) =>
    update.mutate(
      { domainId: d.id, policy: p as OrgDomainPolicy },
      { onSuccess: () => toast.success(p === "auto_join" ? `People at ${d.domain} now join on their own` : `People at ${d.domain} now ask to join`), onError: (e) => toast.error(errorMessage(e, "Couldn't change that. Try again.")) },
    );
  return (
    <li className="flex flex-col">
      <ListRow
        title={d.domain}
        info={
          d.status === "verified"
            ? verifiedInfo(d)
            : d.status === "taken"
              ? `Another ${orgsNoun()} verified this domain with its DNS record. Remove it, or contact support if the domain is yours.`
              : `Added${d.added_by_name ? ` by ${d.added_by_name}` : ""} on ${formatDate(d.created_at)}. It does nothing until it's verified.`
        }
        trailing={
          <span className="flex items-center gap-2">
            <Badge tone={DOMAIN_STATUS_TONE[d.status]}>{DOMAIN_STATUS_LABEL[d.status]}</Badge>
            <Menu align="end" trigger={<IconButton label={`Actions for ${d.domain}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
              <MenuItem destructive icon={<Trash2 aria-hidden />} onSelect={() => onRemove(d)}>
                Remove
              </MenuItem>
            </Menu>
          </span>
        }
      />
      <div className="flex flex-col gap-4 px-4 pb-4">
        <Field label={`People at ${d.domain}`} className="max-w-form" info="Join automatically: no approval needed, and once the DNS record is verified they become members as soon as they open the app. Ask to join: an admin approves each request in Members.">
          <Select value={d.policy} onValueChange={policy} options={DOMAIN_POLICY_OPTIONS} />
        </Field>
      </div>
      {(d.status === "pending" || needsDnsForAutoJoin(d)) && <DnsSteps org={org} d={d} />}
    </li>
  );
}

/** Domains tab (admins): claim email domains, verify them, choose who may join and how. */
export function OrgDomains({ org }: { org: Org }) {
  const me = useMe();
  const domains = useOrgDomains(org.id);
  const remove = useRemoveOrgDomain(org.id);
  const [removing, setRemoving] = useState<OrgDomain | null>(null);
  const rows = domains.data?.items ?? [];
  const myDomain = me.data?.email.split("@")[1];
  return (
    <div className="flex flex-col gap-section">
      <Section
        title="Email domains"
        info={`People whose verified email is on a verified domain find ${org.name} in Settings and join it, or ask to, without an invite. A domain is verified at once when your own email is on it (${myDomain ?? "your domain"}); otherwise add a DNS TXT record.`}
      >
        <AddDomainForm org={org} />
      </Section>
      <Card padding="none" className="p-1">
        {domains.isPending ? (
          <SkeletonGroup label="Loading domains" className="flex flex-col">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : domains.isError ? (
          <ErrorState compact title="Couldn't load domains" info={domains.error.message} onRetry={() => void domains.refetch()} retrying={domains.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<AtSign />} title="No domains yet" />
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {rows.map((d) => (
              <DomainItem key={d.id} org={org} d={d} onRemove={setRemoving} />
            ))}
          </ul>
        )}
      </Card>
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={removing ? `Remove ${removing.domain}?` : ""}
        tone="danger"
        confirmLabel="Remove"
        errorText={(e) => errorMessage(e, "Couldn't remove the domain. Try again.")}
        onConfirm={async () => {
          if (!removing) return;
          await remove.mutateAsync(removing.id);
          toast.success(`${removing.domain} removed`);
        }}
      >
        <p>People with an email on it can no longer join on their own. Current members stay.</p>
      </ConfirmDialog>
    </div>
  );
}
