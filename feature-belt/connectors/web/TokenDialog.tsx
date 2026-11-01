"use client";

import { errorMessage, useStartConnectorConnect, type Connector } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, toast } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useState } from "react";

/**
 * Connect a service that signs in with a token the user creates there (a personal access
 * token). The server checks it against the service before saving it, encrypted.
 */
export function TokenDialog({ connector, onOpenChange }: { connector: Connector | null; onOpenChange: (open: boolean) => void }) {
  const start = useStartConnectorConnect();
  const router = useRouter();
  const [token, setToken] = useState("");
  const close = (o: boolean) => {
    onOpenChange(o);
    if (!o) {
      setToken("");
      start.reset();
    }
  };
  const submit = () => {
    if (!connector || !token.trim() || start.isPending) return;
    start.mutate(
      { id: connector.id, token: token.trim() },
      {
        onSuccess: (r) => {
          toast.success(`Connected to ${connector.name}`);
          close(false);
          const id = r.connector?.connection?.id;
          if (id) router.push(`/settings/connections/${id}`);
        },
      },
    );
  };
  return (
    <Dialog
      open={connector !== null}
      onOpenChange={close}
      title={connector ? `Connect ${connector.name}` : "Connect"}
      footer={
        <>
          <Button variant="secondary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button loading={start.isPending} disabled={!token.trim()} onClick={submit}>
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
        <Field
          label="Token"
          info={`Create a personal access token in ${connector?.name ?? "the service"} with the access you want the assistant to have. It's stored encrypted and used only for your requests.`}
          error={start.error ? errorMessage(start.error) : undefined}
        >
          <Input type="password" autoComplete="off" spellCheck={false} value={token} maxLength={4096} onChange={(e) => setToken(e.target.value)} autoFocus />
        </Field>
      </form>
    </Dialog>
  );
}
