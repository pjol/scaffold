"use client";

import { Button, Card, Field, Input } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader } from "@/components/shell/PageHeader";

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;

/** A conversation id and an optional message id from an id or a `/chat/<id>?message=<id>` link. */
export function parseConversationRef(raw: string): { id: string; around: string | null } | null {
  const ids = raw.trim().match(UUID);
  if (!ids?.[0]) return null;
  return { id: ids[0].toLowerCase(), around: ids[1]?.toLowerCase() ?? null };
}

/** Admin → Chat: open a conversation's staff view from its id or a reported message's link. */
export function AdminChatScreen() {
  const router = useRouter();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Chat" info="Reports link to the message. Opening a conversation here is recorded in the audit log." />
      <Card>
        <form
          className="flex max-w-form flex-col gap-4 sm:flex-row sm:items-end"
          onSubmit={(e) => {
            e.preventDefault();
            const ref = parseConversationRef(value);
            if (!ref) {
              setError("Paste a conversation ID or a /chat link.");
              return;
            }
            router.push(`/admin/chat/${ref.id}${ref.around ? `?around=${ref.around}` : ""}`);
          }}
        >
          <Field label="Conversation ID or link" error={error ?? undefined} className="flex-1">
            <Input
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                setError(null);
              }}
              placeholder="/chat/…?message=…"
            />
          </Field>
          <Button type="submit" variant="secondary">
            Open
          </Button>
        </form>
      </Card>
    </div>
  );
}
