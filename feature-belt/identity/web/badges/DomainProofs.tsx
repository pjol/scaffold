"use client";

import { errorMessage, useAddIdentityDomain, useCheckIdentityDomain, useRemoveIdentityDomain, type IdentityDomain } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, IconButton, Input, toast } from "@scaffoldapp/ui-web";
import { Copy, Globe, Trash2 } from "lucide-react";
import { useState } from "react";
import { Section } from "@/components/shell/PageHeader";

function CopyValue({ text, label }: { text: string; label: string }) {
  return (
    <IconButton
      label={label}
      icon={<Copy />}
      variant="ghost"
      size="sm"
      onClick={() =>
        void navigator.clipboard.writeText(text).then(
          () => toast.success("Copied"),
          () => toast.error("Couldn't copy. Select the text instead."),
        )
      }
    />
  );
}

function DomainCard({ d }: { d: IdentityDomain }) {
  const check = useCheckIdentityDomain();
  const remove = useRemoveIdentityDomain();
  return (
    <Card className="flex flex-col gap-3">
      <div className="flex min-h-control items-center gap-2">
        <Globe className="size-5 shrink-0 text-fg-muted" aria-hidden />
        <span className="min-w-0 flex-1 truncate type-body font-semibold text-fg">{d.domain}</span>
        {d.verified ? (
          <Badge tone="success">Verified</Badge>
        ) : (
          <Button
            size="sm"
            variant="secondary"
            loading={check.isPending}
            onClick={() =>
              check.mutate(d.domain, {
                onSuccess: () => toast.success(`${d.domain} verified`),
                onError: (e) => toast.error(errorMessage(e, "Couldn't check the domain. Try again.")),
              })
            }
          >
            Check
          </Button>
        )}
        <ConfirmDialog
          title={`Remove ${d.domain}?`}
          confirmLabel="Remove"
          tone="danger"
          trigger={<IconButton label="Remove domain" icon={<Trash2 />} variant="ghost" size="sm" />}
          onConfirm={() => remove.mutateAsync(d.domain)}
        >
          Applications you already sent keep it as evidence.
        </ConfirmDialog>
      </div>
      {!d.verified ? (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 type-label">
          <dt className="text-fg-muted">Type</dt>
          <dd className="text-fg">TXT</dd>
          <span />
          <dt className="text-fg-muted">Name</dt>
          <dd className="truncate font-mono text-fg">{d.txt_name}</dd>
          <CopyValue text={d.txt_name} label="Copy record name" />
          <dt className="text-fg-muted">Value</dt>
          <dd className="truncate font-mono text-fg">{d.txt_value}</dd>
          <CopyValue text={d.txt_value} label="Copy record value" />
        </dl>
      ) : null}
      {!d.verified && d.last_error ? <Banner tone="warning">{d.last_error}</Banner> : null}
    </Card>
  );
}

/** Domain proofs: add a domain, put the TXT record in its DNS, check it. */
export function DomainProofs({ domains }: { domains: IdentityDomain[] }) {
  const add = useAddIdentityDomain();
  const [domain, setDomain] = useState("");
  return (
    <Section
      title="Domains"
      info="Prove you run a website: add the TXT record below in your domain's DNS settings, then check it. The record can also go on the domain itself. Public email and hosting domains don't count."
    >
      <form
        className="flex max-w-form gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          add.mutate(domain.trim(), {
            onSuccess: () => setDomain(""),
            onError: (err) => toast.error(errorMessage(err, "Couldn't add the domain. Try again.")),
          });
        }}
      >
        <Input aria-label="Domain" wrapperClassName="min-w-0 flex-1" placeholder="example.com" inputMode="url" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={domain} onChange={(e) => setDomain(e.target.value)} />
        <Button type="submit" variant="secondary" size="auto" loading={add.isPending} disabled={domain.trim().length < 4}>
          Add
        </Button>
      </form>
      {domains.map((d) => (
        <DomainCard key={d.domain} d={d} />
      ))}
    </Section>
  );
}
