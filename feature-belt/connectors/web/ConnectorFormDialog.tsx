"use client";

import {
  connectorRegistrationLabel,
  errorMessage,
  useCheckConnectorServer,
  useCreateAdminConnector,
  useDeleteAdminConnector,
  useUpdateAdminConnector,
  type AdminConnector,
  type AdminConnectorInput,
  type ConnectorAuthType,
  type ConnectorCheckResult,
  type ConnectorWritePolicy,
} from "@scaffoldapp/shared";
import { Banner, Button, Checkbox, ConfirmDialog, Dialog, Field, Input, SegmentedControl, Select, Switch, TextArea, toast } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";

const AUTH_OPTIONS: { value: ConnectorAuthType; label: string }[] = [
  { value: "oauth", label: "OAuth" },
  { value: "bearer", label: "Pasted token" },
];
const POLICY_OPTIONS: { value: ConnectorWritePolicy; label: string }[] = [
  { value: "off", label: "Allowed, off by default" },
  { value: "on", label: "Allowed, on by default" },
  { value: "disabled", label: "Never" },
];

interface Draft {
  name: string;
  slug: string;
  description: string;
  icon_url: string;
  server_url: string;
  auth_type: ConnectorAuthType;
  oauth_client_id: string;
  oauth_client_secret: string;
  clear_client_secret: boolean;
  oauth_scopes: string;
  write_policy: ConnectorWritePolicy;
  enabled: boolean;
}

function draftOf(c: AdminConnector | null): Draft {
  return {
    name: c?.name ?? "",
    slug: c?.slug ?? "",
    description: c?.description ?? "",
    icon_url: c?.icon_url ?? "",
    server_url: c?.server_url ?? "",
    auth_type: c?.auth_type ?? "oauth",
    oauth_client_id: c?.oauth_client_id ?? "",
    oauth_client_secret: "",
    clear_client_secret: false,
    oauth_scopes: c?.oauth_scopes ?? "",
    write_policy: c?.write_policy ?? "off",
    enabled: c?.enabled ?? true,
  };
}

/** Add or edit a catalog entry. Check server runs discovery so staff know whether a client ID is needed. */
export function ConnectorFormDialog({ connector, open, onOpenChange }: { connector: AdminConnector | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const create = useCreateAdminConnector();
  const update = useUpdateAdminConnector();
  const remove = useDeleteAdminConnector();
  const check = useCheckConnectorServer();
  const [d, setD] = useState<Draft>(() => draftOf(connector));
  const [deleting, setDeleting] = useState(false);
  const editing = connector !== null;

  useEffect(() => {
    if (open) {
      setD(draftOf(connector));
      check.reset();
      create.reset();
      update.reset();
    }
    // Reset only when the dialog opens for a (new) target.
  }, [open, connector?.id]);

  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((p) => ({ ...p, [k]: v }));
  const saving = create.isPending || update.isPending;
  const error = create.error ?? update.error;

  const body = (): AdminConnectorInput => {
    const b: AdminConnectorInput = {
      name: d.name.trim(),
      description: d.description.trim(),
      icon_url: d.icon_url.trim(),
      server_url: d.server_url.trim(),
      auth_type: d.auth_type,
      oauth_client_id: d.auth_type === "oauth" ? d.oauth_client_id.trim() : "",
      oauth_scopes: d.auth_type === "oauth" ? d.oauth_scopes.trim() : "",
      write_policy: d.write_policy,
      enabled: d.enabled,
    };
    if (!editing && d.slug.trim()) b.slug = d.slug.trim();
    if (d.auth_type === "oauth" && d.oauth_client_secret) b.oauth_client_secret = d.oauth_client_secret;
    if (d.clear_client_secret || d.auth_type === "bearer") b.clear_client_secret = true;
    return b;
  };

  const save = () => {
    if (!d.name.trim() || !d.server_url.trim() || saving) return;
    const done = (name: string) => {
      toast.success(editing ? `${name} saved` : `${name} added`);
      onOpenChange(false);
    };
    if (connector) update.mutate({ id: connector.id, body: body() }, { onSuccess: (c) => done(c.name) });
    else create.mutate(body(), { onSuccess: (c) => done(c.name) });
  };

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title={editing ? `Edit ${connector.name}` : "Add connector"}
        size="lg"
        footer={
          <>
            {editing ? (
              <Button variant="danger-ghost" className="sm:mr-auto" onClick={() => setDeleting(true)}>
                Delete
              </Button>
            ) : null}
            <Button variant="secondary" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button loading={saving} disabled={!d.name.trim() || !d.server_url.trim()} onClick={save}>
              {editing ? "Save" : "Add connector"}
            </Button>
          </>
        }
      >
        <form
          className="flex flex-col gap-6"
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          {error ? <Banner tone="danger">{errorMessage(error)}</Banner> : null}
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-4 sm:flex-row">
              <Field label="Name" className="sm:flex-1">
                <Input value={d.name} maxLength={60} onChange={(e) => set("name", e.target.value)} placeholder="Linear" autoFocus={!editing} />
              </Field>
              <Field
                label="Slug"
                className="sm:w-48"
                info={editing ? "Tool names start with it, so it can't change." : "Tool names start with it (linear__list_issues). Lowercase letters and digits; leave empty to use the name."}
              >
                <Input value={d.slug} maxLength={20} disabled={editing} spellCheck={false} onChange={(e) => set("slug", e.target.value.toLowerCase())} placeholder="linear" />
              </Field>
            </div>
            <Field label="Description" info="Shown to people next to the name.">
              <TextArea value={d.description} maxLength={300} minRows={2} onChange={(e) => set("description", e.target.value)} />
            </Field>
            <Field label="Icon URL" info="An https image, shown at 40 px. Leave empty to show the first letter.">
              <Input type="url" inputMode="url" spellCheck={false} value={d.icon_url} maxLength={500} onChange={(e) => set("icon_url", e.target.value)} placeholder="https://" />
            </Field>
          </div>

          <div className="flex flex-col gap-4">
            <Field label="Server URL" info="The MCP endpoint (streamable HTTP), for example https://mcp.linear.app/mcp.">
              <Input
                type="url"
                inputMode="url"
                spellCheck={false}
                value={d.server_url}
                maxLength={500}
                onChange={(e) => set("server_url", e.target.value)}
                placeholder="https://"
                trailing={
                  <Button
                    size="sm"
                    variant="ghost"
                    loading={check.isPending}
                    disabled={!d.server_url.trim()}
                    onClick={() =>
                      check.mutate(
                        { server_url: d.server_url.trim(), auth_type: d.auth_type, ...(d.oauth_client_id.trim() ? { oauth_client_id: d.oauth_client_id.trim() } : {}) },
                        { onError: (e) => toast.error(errorMessage(e, "Couldn't check the server. Try again.")) },
                      )
                    }
                  >
                    Check
                  </Button>
                }
              />
            </Field>
            {check.data ? <CheckFacts result={check.data} /> : null}
            <Field label="Sign-in" info="OAuth: each person signs in on the service. Pasted token: each person pastes a personal access token.">
              <SegmentedControl aria-label="Sign-in" options={AUTH_OPTIONS} value={d.auth_type} onValueChange={(v) => set("auth_type", v)} />
            </Field>
            {d.auth_type === "oauth" ? (
              <>
                <Field label="Client ID" info="Only for services without dynamic client registration: register the app there with the redirect URL /v1/connectors/oauth/callback on this API.">
                  <Input value={d.oauth_client_id} maxLength={300} spellCheck={false} onChange={(e) => set("oauth_client_id", e.target.value)} placeholder="Optional" />
                </Field>
                <Field label="Client secret" info="Stored encrypted and never shown again.">
                  <Input
                    type="password"
                    autoComplete="off"
                    value={d.oauth_client_secret}
                    maxLength={500}
                    onChange={(e) => set("oauth_client_secret", e.target.value)}
                    placeholder={connector?.has_client_secret ? "Saved. Enter a new one to replace it" : "Optional"}
                  />
                </Field>
                {connector?.has_client_secret ? (
                  <Checkbox checked={d.clear_client_secret} onCheckedChange={(v) => set("clear_client_secret", v === true)} label="Remove the saved secret" />
                ) : null}
                <Field label="Scopes" info="Space-separated. Leave empty to ask for what the server advertises.">
                  <Input value={d.oauth_scopes} maxLength={500} spellCheck={false} onChange={(e) => set("oauth_scopes", e.target.value)} placeholder="Optional" />
                </Field>
              </>
            ) : null}
          </div>

          <div className="flex flex-col gap-4">
            <Field label="Changes" info="Whether tools that create, edit or delete can be offered. People still approve each change in the assistant.">
              <Select options={POLICY_OPTIONS} value={d.write_policy} onValueChange={(v) => set("write_policy", v as ConnectorWritePolicy)} />
            </Field>
            <Switch label="Available to connect" info="Off hides it from people and stops its tools; existing connections stay so people can remove them." checked={d.enabled} onCheckedChange={(v) => set("enabled", v)} />
          </div>
        </form>
      </Dialog>
      {connector ? (
        <ConfirmDialog
          open={deleting}
          onOpenChange={setDeleting}
          title={`Delete ${connector.name}?`}
          confirmLabel="Delete"
          tone="danger"
          onConfirm={async () => {
            await remove.mutateAsync(connector.id);
            toast.success(`${connector.name} deleted`);
            setDeleting(false);
            onOpenChange(false);
          }}
          errorText={(e) => errorMessage(e, "Couldn't delete. Try again.")}
        >
          {connector.connections === 1
            ? "1 person loses their connection and its access is revoked. The action is recorded in the audit log."
            : `${connector.connections} people lose their connection and their access is revoked. The action is recorded in the audit log.`}
        </ConfirmDialog>
      ) : null}
    </>
  );
}

function CheckFacts({ result: r }: { result: ConnectorCheckResult }) {
  if (!r.ok) return <Banner tone="danger">{r.error || "The server didn't pass the check."}</Banner>;
  const facts: [string, string][] = [
    ["Registration", connectorRegistrationLabel(r.registration)],
    ...(r.issuer ? ([["Authorization server", r.issuer]] as [string, string][]) : []),
    ["Scopes", r.scopes.length ? r.scopes.join(" ") : "None advertised"],
    ["Refresh tokens", r.refresh ? "Yes" : "No"],
    ["Revocation", r.revocation ? "Supported" : "Not supported"],
  ];
  return (
    <div className="flex flex-col gap-2">
      {r.registration === "needs_client_id" ? <Banner tone="warning">This service needs a client ID registered with it. Add it below.</Banner> : null}
      <dl className="flex flex-col gap-1 rounded-md bg-sunken px-4 py-3 type-label">
        {facts.map(([k, v]) => (
          <div key={k} className="flex flex-wrap gap-x-3">
            <dt className="text-fg-muted">{k}</dt>
            <dd className="min-w-0 break-all text-fg">{v}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
