"use client";

import {
  errorMessage,
  hasErrorCode,
  useCreateInvite,
  useCreateLink,
  useOrgInvites,
  useOrgLinks,
  useResendInvite,
  useRevokeInvite,
  useRevokeLink,
  type Org,
  type OrgInviteLink,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, IconButton, Input, ListRow, Menu, MenuItem, Select, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Copy, Link2, Mail, MoreHorizontal, Plus, RotateCw, Send, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success("Link copied");
  } catch {
    toast.error("Couldn't copy. Select the link and copy it.");
  }
}

function InviteForm({ org }: { org: Org }) {
  const create = useCreateInvite(org.id);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"member" | "admin">("member");
  const [error, setError] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const value = email.trim().toLowerCase();
    if (!EMAIL.test(value)) return setError("Enter a valid email address.");
    setError(null);
    create.mutate(
      { email: value, role },
      {
        onSuccess: () => {
          toast.success(`Invite sent to ${value}`);
          setEmail("");
        },
        onError: (err) => (hasErrorCode(err, "conflict") || hasErrorCode(err, "invalid_request") ? setError(errorMessage(err)) : toast.error(errorMessage(err, "Couldn't send the invite. Try again."))),
      },
    );
  };
  return (
    <form className="flex max-w-form flex-col gap-3 sm:flex-row sm:items-end" onSubmit={submit} noValidate>
      <Field label="Email" error={error ?? undefined} className="flex-1">
        <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@example.com" autoComplete="off" />
      </Field>
      <Field label="Role" className="sm:w-36">
        <Select
          value={role}
          onValueChange={(v) => setRole(v as "member" | "admin")}
          options={[
            { value: "member", label: "Member" },
            { value: "admin", label: "Admin" },
          ]}
        />
      </Field>
      <Button type="submit" size="auto" leftIcon={<Send />} loading={create.isPending}>
        Send invite
      </Button>
    </form>
  );
}

function PendingInvites({ org }: { org: Org }) {
  const invites = useOrgInvites(org.id);
  const resend = useResendInvite(org.id);
  const revoke = useRevokeInvite(org.id);
  const rows = invites.data?.items ?? [];
  return (
    <Card padding="none" className="p-1">
      {invites.isPending ? (
        <SkeletonGroup label="Loading invites" className="flex flex-col">
          <SkeletonRow leading={false} />
          <SkeletonRow leading={false} />
        </SkeletonGroup>
      ) : invites.isError ? (
        <ErrorState compact title="Couldn't load invites" info={invites.error.message} onRetry={() => void invites.refetch()} retrying={invites.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Mail />} title="No pending invites" />
      ) : (
        <ul className="flex flex-col">
          {rows.map((inv) => (
            <li key={inv.id}>
              <ListRow
                title={inv.email}
                info={`${inv.invited_by_name ? `Sent by ${inv.invited_by_name}` : "Sent"}${inv.sent_count > 1 ? ` ${inv.sent_count} times` : ""}. Expires ${formatDate(inv.expires_at)}.`}
                trailing={
                  <span className="flex items-center gap-2">
                    <Badge tone={inv.role === "admin" ? "info" : "neutral"}>{inv.role === "admin" ? "Admin" : "Member"}</Badge>
                    <Menu align="end" trigger={<IconButton label={`Actions for ${inv.email}`} icon={<MoreHorizontal />} variant="ghost" size="sm" />}>
                      <MenuItem
                        icon={<RotateCw aria-hidden />}
                        onSelect={() =>
                          resend.mutate(inv.id, {
                            onSuccess: () => toast.success(`Invite sent again to ${inv.email}`),
                            onError: (e) => toast.error(errorMessage(e, "Couldn't resend. Try again.")),
                          })
                        }
                      >
                        Resend
                      </MenuItem>
                      <MenuItem
                        destructive
                        icon={<X aria-hidden />}
                        onSelect={() =>
                          revoke.mutate(inv.id, {
                            onSuccess: () => toast.success("Invite revoked"),
                            onError: (e) => toast.error(errorMessage(e, "Couldn't revoke. Try again.")),
                          })
                        }
                      >
                        Revoke
                      </MenuItem>
                    </Menu>
                  </span>
                }
              />
            </li>
          ))}
        </ul>
      )}
      <LoadMore hasNextPage={invites.hasNextPage} isFetchingNextPage={invites.isFetchingNextPage} fetchNextPage={invites.fetchNextPage} />
    </Card>
  );
}

function linkSummary(l: OrgInviteLink): string {
  const uses = l.max_uses ? `${l.uses} of ${l.max_uses} uses` : `${l.uses} ${l.uses === 1 ? "use" : "uses"}`;
  const expiry = l.expires_at ? `, expires ${formatDate(l.expires_at)}` : "";
  return `${uses}${expiry}`;
}

function NewLinkDialog({ org, open, onOpenChange }: { org: Org; open: boolean; onOpenChange: (open: boolean) => void }) {
  const create = useCreateLink(org.id);
  const [maxUses, setMaxUses] = useState("");
  const [days, setDays] = useState("7");
  const [created, setCreated] = useState<OrgInviteLink | null>(null);
  const [error, setError] = useState<string | null>(null);
  const close = (o: boolean) => {
    if (create.isPending) return;
    onOpenChange(o);
    if (!o) {
      setCreated(null);
      setMaxUses("");
      setDays("7");
      setError(null);
    }
  };
  const submit = async () => {
    const uses = maxUses.trim() ? Number(maxUses) : undefined;
    if (uses !== undefined && (!Number.isInteger(uses) || uses < 1 || uses > 10000)) return setError("Uses must be a whole number from 1 to 10000.");
    setError(null);
    try {
      const link = await create.mutateAsync({ ...(uses ? { max_uses: uses } : {}), ...(days !== "never" ? { expires_in_days: Number(days) } : {}) });
      setCreated(link);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't make the link. Try again."));
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title={created ? "Link ready" : "New invite link"}
      info="Anyone signed in who opens the link joins as a member. Revoke it when you're done sharing."
      footer={
        created ? (
          <Button size="auto" onClick={() => close(false)}>
            Done
          </Button>
        ) : (
          <>
            <Button variant="ghost" size="auto" disabled={create.isPending} onClick={() => close(false)}>
              Cancel
            </Button>
            <Button size="auto" loading={create.isPending} onClick={() => void submit()}>
              Make link
            </Button>
          </>
        )
      }
    >
      {created?.url ? (
        <div className="flex flex-col gap-3">
          <Field label="Link" info="Copy it now: it isn't shown again.">
            <Input value={created.url} readOnly onFocus={(e) => e.currentTarget.select()} trailing={<IconButton label="Copy link" icon={<Copy />} size="sm" variant="ghost" onClick={() => void copy(created.url ?? "")} />} />
          </Field>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <Field label="Expires" info="After this, the link stops working.">
            <Select
              value={days}
              onValueChange={setDays}
              options={[
                { value: "1", label: "In 1 day" },
                { value: "7", label: "In 7 days" },
                { value: "30", label: "In 30 days" },
                { value: "never", label: "Never" },
              ]}
            />
          </Field>
          <Field label="Most uses" info="Empty: no limit." error={error ?? undefined}>
            <Input inputMode="numeric" value={maxUses} onChange={(e) => setMaxUses(e.target.value.replace(/\D/g, ""))} placeholder="No limit" />
          </Field>
        </div>
      )}
    </Dialog>
  );
}

function Links({ org }: { org: Org }) {
  const links = useOrgLinks(org.id);
  const revoke = useRevokeLink(org.id);
  const [creating, setCreating] = useState(false);
  const rows = links.data?.items ?? [];
  return (
    <Section
      title="Invite links"
      info="Share a link in a group chat or email. Links add members; promote people afterwards."
      action={
        <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={() => setCreating(true)}>
          New link
        </Button>
      }
    >
      <Card padding="none" className="p-1">
        {links.isPending ? (
          <SkeletonGroup label="Loading links" className="flex flex-col">
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : links.isError ? (
          <ErrorState compact title="Couldn't load links" info={links.error.message} onRetry={() => void links.refetch()} retrying={links.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Link2 />} title="No invite links" />
        ) : (
          <ul className="flex flex-col">
            {rows.map((l) => (
              <li key={l.id}>
                <ListRow
                  title={`Link from ${formatDate(l.created_at)}`}
                  info={`${linkSummary(l)}${l.created_by_name ? `. Made by ${l.created_by_name}` : ""}.`}
                  trailing={
                    <span className="flex items-center gap-2">
                      <Badge tone={l.active ? "success" : "neutral"}>{l.revoked_at ? "Revoked" : l.active ? "Active" : "Used up or expired"}</Badge>
                      {l.revoked_at === null && (
                        <Button
                          variant="danger-ghost"
                          size="sm"
                          loading={revoke.isPending && revoke.variables === l.id}
                          onClick={() => revoke.mutate(l.id, { onSuccess: () => toast.success("Link revoked"), onError: (e) => toast.error(errorMessage(e, "Couldn't revoke. Try again.")) })}
                        >
                          Revoke
                        </Button>
                      )}
                    </span>
                  }
                />
              </li>
            ))}
          </ul>
        )}
        <LoadMore hasNextPage={links.hasNextPage} isFetchingNextPage={links.isFetchingNextPage} fetchNextPage={links.fetchNextPage} />
      </Card>
      <NewLinkDialog org={org} open={creating} onOpenChange={setCreating} />
    </Section>
  );
}

/** Invites tab (admins): invite by email, pending invites (resend, revoke), shareable links. */
export function OrgInvites({ org }: { org: Org }) {
  return (
    <div className="flex flex-col gap-section">
      <Section title="Invite by email" info="They get an email with a link that works for 7 days, only for that address.">
        <InviteForm org={org} />
      </Section>
      <Section title="Pending invites">
        <PendingInvites org={org} />
      </Section>
      <Links org={org} />
    </div>
  );
}
