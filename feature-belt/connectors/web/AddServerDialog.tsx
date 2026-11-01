"use client";

import { errorMessage, useCreateCustomConnector, useStartConnectorConnect, type Connector, type ConnectorAuthType } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, SegmentedControl, toast } from "@scaffoldapp/ui-web";
import { useState } from "react";

const AUTH_OPTIONS: { value: ConnectorAuthType; label: string }[] = [
  { value: "oauth", label: "Sign in" },
  { value: "bearer", label: "Token" },
];

/**
 * Add your own MCP server by URL (when the server allows it). A sign-in server goes
 * straight to its sign-in page; a token server asks for the token next (onCreated).
 */
export function AddServerDialog({ open, onOpenChange, onCreated }: { open: boolean; onOpenChange: (open: boolean) => void; onCreated: (c: Connector) => void }) {
  const create = useCreateCustomConnector();
  const start = useStartConnectorConnect();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [auth, setAuth] = useState<ConnectorAuthType>("oauth");
  const [redirecting, setRedirecting] = useState(false);
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setName("");
      setUrl("");
      setAuth("oauth");
      create.reset();
      start.reset();
    }
  };
  const busy = create.isPending || start.isPending || redirecting;
  const submit = () => {
    if (!name.trim() || !url.trim() || busy) return;
    create.mutate(
      { name: name.trim(), server_url: url.trim(), auth_type: auth },
      {
        onSuccess: (c) => {
          if (c.auth_type === "bearer") {
            close(false);
            onCreated(c);
            return;
          }
          start.mutate(
            { id: c.id, client: "web" },
            {
              onSuccess: (r) => {
                if (r.authorize_url) {
                  setRedirecting(true);
                  window.location.assign(r.authorize_url);
                }
              },
              onError: (e) => {
                toast.error(errorMessage(e, "Saved, but couldn't start the sign-in. Use Connect to try again."));
                close(false);
              },
            },
          );
        },
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={close}
      title="Add a server"
      info="Any MCP server that signs you in with your own account. Everything it offers counts as able to change things, so the assistant asks before each use once you allow changes."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button loading={busy} disabled={!name.trim() || !url.trim()} onClick={submit}>
            {auth === "oauth" ? "Add and sign in" : "Add"}
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
        <Field label="Name">
          <Input value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder="Team wiki" autoFocus />
        </Field>
        <Field label="Server URL" info="The server's MCP endpoint, for example https://mcp.example.com/mcp." error={create.error ? errorMessage(create.error) : undefined}>
          <Input type="url" inputMode="url" spellCheck={false} value={url} maxLength={500} onChange={(e) => setUrl(e.target.value)} placeholder="https://" />
        </Field>
        <Field label="How it signs you in" info="Sign in opens the service's own sign-in page. Token: you paste a personal access token from the service.">
          <SegmentedControl aria-label="How it signs you in" options={AUTH_OPTIONS} value={auth} onValueChange={setAuth} />
        </Field>
      </form>
    </Dialog>
  );
}
