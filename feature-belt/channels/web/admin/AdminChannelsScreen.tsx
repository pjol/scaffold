"use client";

import { Button, Card, Field, Input } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader } from "@/components/shell/PageHeader";

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

/** A channel id, thread and message from an id or a `/channels/<id>?thread=…&message=…` link. */
export function parseChannelRef(raw: string): { id: string; thread: string | null; around: string | null } | null {
  const text = raw.trim();
  const id = UUID.exec(text)?.[0];
  if (!id) return null;
  const param = (name: string) => new RegExp(`[?&]${name}=(${UUID.source})`, "i").exec(text)?.[1]?.toLowerCase() ?? null;
  return { id: id.toLowerCase(), thread: param("thread"), around: param("message") };
}

/** Admin → Channels: open a channel's staff view from its id or a reported message's link. */
export function AdminChannelsScreen() {
  const router = useRouter();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Channels" info="Reports link to the message. Opening a channel here is recorded in the audit log." />
      <Card>
        <form
          className="flex max-w-form flex-col gap-4 sm:flex-row sm:items-end"
          onSubmit={(e) => {
            e.preventDefault();
            const ref = parseChannelRef(value);
            if (!ref) {
              setError("Paste a channel ID or a /channels link.");
              return;
            }
            const q = [ref.around && `around=${ref.around}`, ref.thread && `thread=${ref.thread}`].filter(Boolean).join("&");
            router.push(`/admin/channels/${ref.id}${q ? `?${q}` : ""}`);
          }}
        >
          <Field label="Channel ID or link" error={error ?? undefined} className="flex-1">
            <Input
              value={value}
              onChange={(e) => {
                setValue(e.target.value);
                setError(null);
              }}
              placeholder="/channels/…?message=…"
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
