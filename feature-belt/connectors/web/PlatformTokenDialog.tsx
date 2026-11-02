"use client";

import { errorMessage, useConnectorPlatformToken, type ConnectorPlatform } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, toast } from "@scaffoldapp/ui-web";
import { useState } from "react";

/**
 * Connect a platform with a personal or private token (GitHub, GitLab incl. self-managed,
 * Notion internal integrations, HubSpot private apps). The server checks it with the
 * platform, which names the account, before storing it encrypted.
 */
export function PlatformTokenDialog({
  platform,
  capability,
  orgId,
  onOpenChange,
  onConnected,
}: {
  platform: ConnectorPlatform | null;
  capability?: string | undefined;
  orgId: string | null;
  onOpenChange: (open: boolean) => void;
  onConnected?: () => void;
}) {
  const save = useConnectorPlatformToken();
  const [token, setToken] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setToken("");
      setBaseUrl("");
      save.reset();
    }
  };
  const t = platform?.token;
  const submit = () => {
    if (!platform || !token.trim() || save.isPending) return;
    save.mutate(
      {
        provider: platform.id,
        token: token.trim(),
        ...(baseUrl.trim() ? { base_url: baseUrl.trim() } : {}),
        ...(capability ? { capability } : {}),
        ...(orgId ? { org_id: orgId } : {}),
      },
      {
        onSuccess: (r) => {
          toast.success(`Connected ${platform.name}${r.connection ? ` · ${r.connection.account}` : ""}`);
          close(false);
          onConnected?.();
        },
      },
    );
  };
  return (
    <Dialog
      open={platform !== null}
      onOpenChange={close}
      title={platform ? `Connect ${platform.name} with a token` : "Connect with a token"}
      footer={
        <>
          <Button variant="secondary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button loading={save.isPending} disabled={!token.trim()} onClick={submit}>
            Connect
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
        {t?.base_url_editable ? (
          <Field label="Instance" info={`Leave empty for ${platform?.base_url ?? "the default instance"}. For a self-managed instance, enter its address.`}>
            <Input type="url" inputMode="url" autoComplete="off" spellCheck={false} placeholder={platform?.base_url ?? "https://gitlab.example.com"} value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />
          </Field>
        ) : null}
        <Field
          label={t?.label ?? "Token"}
          info={
            <span>
              Create one in {platform?.name ?? "the platform"} with the access you want here
              {t?.help_url ? (
                <>
                  {" "}
                  (
                  <a href={t.help_url} target="_blank" rel="noreferrer" className="underline underline-offset-2">
                    open {platform?.name}
                  </a>
                  )
                </>
              ) : null}
              . It's stored encrypted and used only for your requests; {platform?.name ?? "the platform"} decides what it can reach.
            </span>
          }
          error={save.error ? errorMessage(save.error) : undefined}
        >
          <Input type="password" autoComplete="off" spellCheck={false} value={token} maxLength={4096} onChange={(e) => setToken(e.target.value)} autoFocus={!t?.base_url_editable} />
        </Field>
      </form>
    </Dialog>
  );
}
