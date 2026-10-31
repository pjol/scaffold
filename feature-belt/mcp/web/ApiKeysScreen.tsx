"use client";

import { errorMessage, mcpEndpointUrl, useCreateMcpKey, useMcpKeys, useRevokeMcpKey, type McpApiKey, type McpCreatedKey, type McpKeyScope } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, ConfirmDialog, Dialog, Divider, EmptyState, ErrorState, Field, IconButton, Input, SegmentedControl, SkeletonRow, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Check, Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { Fragment, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { API_URL } from "@/lib/env";
import { formatDate, formatRelative } from "@/lib/format";

const MAX_KEYS = 10;

/**
 * Settings → API keys: personal keys that let agents (Claude, ChatGPT, scripts) call the
 * app's tools over MCP as you. The secret shows once, at creation.
 */
export function ApiKeysScreen() {
  const keys = useMcpKeys();
  const revoke = useRevokeMcpKey();
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<McpApiKey | null>(null);
  const rows = keys.data?.items ?? [];
  const full = rows.length >= MAX_KEYS;
  const endpoint = mcpEndpointUrl(API_URL);

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="API keys"
        info="Agents use a key to read (and, with write access, change) your data through the app's MCP endpoint. Keys act as you; anyone holding one can too."
        action={
          <Button size="sm" leftIcon={<Plus />} disabled={full || keys.isPending} onClick={() => setCreating(true)}>
            Create key
          </Button>
        }
      />
      <Section title="Endpoint" info="Add it to your agent as a remote MCP server with the header Authorization: Bearer <your key>.">
        <CopyField label="MCP endpoint" value={endpoint} className="max-w-form" />
      </Section>
      <Section title="Your keys" info={`Up to ${MAX_KEYS} keys. Revoke one you no longer use.`}>
        <Card padding="none">
          {keys.isPending ? (
            <SkeletonGroup className="flex flex-col">
              <SkeletonRow />
              <SkeletonRow />
            </SkeletonGroup>
          ) : keys.isError ? (
            <ErrorState compact title="Couldn't load your keys" onRetry={() => void keys.refetch()} retrying={keys.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState
              compact
              icon={<KeyRound />}
              title="No API keys"
              action={
                <Button size="sm" variant="secondary" leftIcon={<Plus />} onClick={() => setCreating(true)}>
                  Create key
                </Button>
              }
            />
          ) : (
            <ul>
              {rows.map((k, i) => (
                <Fragment key={k.id}>
                  {i > 0 ? <Divider className="mx-4" /> : null}
                  <li className="flex items-center gap-3 px-4 py-3">
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate type-body font-semibold text-fg">{k.name}</span>
                        <Badge size="sm" tone={k.scopes.includes("write") ? "warning" : "neutral"}>
                          {k.scopes.includes("write") ? "Read and write" : "Read only"}
                        </Badge>
                      </div>
                      <span className="flex flex-wrap gap-x-3 type-caption text-fg-muted">
                        <code className="font-mono">{k.prefix}…</code>
                        <span>Created {formatDate(k.created_at)}</span>
                        <span>{k.last_used_at ? `Used ${formatRelative(k.last_used_at)}` : "Never used"}</span>
                      </span>
                    </div>
                    <IconButton label={`Revoke ${k.name}`} tooltip variant="danger" icon={<Trash2 />} onClick={() => setRevoking(k)} />
                  </li>
                </Fragment>
              ))}
              <LoadMore hasNextPage={keys.hasNextPage} isFetchingNextPage={keys.isFetchingNextPage} fetchNextPage={keys.fetchNextPage} />
            </ul>
          )}
        </Card>
      </Section>
      <CreateKeyDialog open={creating} onOpenChange={setCreating} endpoint={endpoint} />
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(o) => !o && setRevoking(null)}
        title={`Revoke ${revoking?.name ?? "this key"}?`}
        confirmLabel="Revoke"
        tone="danger"
        onConfirm={async () => {
          if (!revoking) return;
          await revoke.mutateAsync(revoking.id);
          toast.success("Key revoked");
          setRevoking(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't revoke the key. Try again.")}
      >
        Agents using it stop working right away. This can't be undone.
      </ConfirmDialog>
    </div>
  );
}

const SCOPES: { value: "read" | "write"; label: string }[] = [
  { value: "read", label: "Read only" },
  { value: "write", label: "Read and write" },
];

function CreateKeyDialog({ open, onOpenChange, endpoint }: { open: boolean; onOpenChange: (open: boolean) => void; endpoint: string }) {
  const create = useCreateMcpKey();
  const [name, setName] = useState("");
  const [access, setAccess] = useState<"read" | "write">("read");
  const [created, setCreated] = useState<McpCreatedKey | null>(null);
  const trimmed = name.trim();

  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      // Forget the secret as soon as the dialog closes.
      setCreated(null);
      setName("");
      setAccess("read");
      create.reset();
    }
  };

  const submit = () => {
    if (!trimmed || create.isPending) return;
    const scopes: McpKeyScope[] = access === "write" ? ["read", "write"] : ["read"];
    create.mutate({ name: trimmed, scopes }, { onSuccess: setCreated });
  };

  if (created) {
    return (
      <Dialog
        open={open}
        onOpenChange={close}
        title="Copy your key"
        footer={
          <Button onClick={() => close(false)} leftIcon={<Check />}>
            Done
          </Button>
        }
      >
        <div className="flex flex-col gap-4">
          <Banner tone="warning">This is the only time the key is shown. Store it somewhere safe.</Banner>
          <CopyField label={created.key.name} value={created.secret} mono />
          <CopyField label="MCP endpoint" value={endpoint} />
        </div>
      </Dialog>
    );
  }
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title="Create API key"
      footer={
        <>
          <Button variant="secondary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button loading={create.isPending} disabled={!trimmed} onClick={submit}>
            Create key
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" info="Where you'll use it, so you can tell keys apart: Claude, laptop script." error={create.error ? errorMessage(create.error) : undefined}>
          <Input value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder="Claude" autoFocus />
        </Field>
        <Field label="Access" info="Read and write also allows tools that change your data.">
          <SegmentedControl aria-label="Access" options={SCOPES} value={access} onValueChange={setAccess} />
        </Field>
      </form>
    </Dialog>
  );
}

function CopyField({ label, value, mono = false, className }: { label: string; value: string; mono?: boolean; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Field label={label} {...(className ? { className } : {})}>
      <Input
        readOnly
        value={value}
        className={mono ? "font-mono" : undefined}
        onFocus={(e) => e.currentTarget.select()}
        trailing={
          <IconButton
            label={copied ? "Copied" : `Copy ${label}`}
            size="sm"
            variant="ghost"
            icon={copied ? <Check /> : <Copy />}
            onClick={() => {
              void navigator.clipboard.writeText(value).then(
                () => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                },
                () => toast.error("Couldn't copy. Select the text instead."),
              );
            }}
          />
        }
      />
    </Field>
  );
}
