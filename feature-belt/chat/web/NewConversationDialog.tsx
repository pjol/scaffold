"use client";

import { errorMessage, useCreateConversation, type ChatUser } from "@scaffoldapp/shared";
import { Banner, Button, Chip, Dialog, Field, Input } from "@scaffoldapp/ui-web";
import { X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { PeoplePicker } from "./PeoplePicker";

export type NewConversationMode = "direct" | "group";

/** Start a direct conversation (pick one person) or a group (name + people). */
export function NewConversationDialog({ mode, onOpenChange }: { mode: NewConversationMode | null; onOpenChange: (open: boolean) => void }) {
  const router = useRouter();
  const create = useCreateConversation();
  const [title, setTitle] = useState("");
  const [members, setMembers] = useState<ChatUser[]>([]);

  useEffect(() => {
    if (mode) {
      setTitle("");
      setMembers([]);
      create.reset();
    }
    // Reset when the dialog opens, not when the mutation object changes.
  }, [mode]); // eslint-disable-line react-hooks/exhaustive-deps

  const open = (id: string) => {
    onOpenChange(false);
    router.push(`/chat/${id}`);
  };
  const toggle = (u: ChatUser) => setMembers((list) => (list.some((x) => x.id === u.id) ? list.filter((x) => x.id !== u.id) : [...list, u]));
  const canCreate = title.trim().length > 0 && members.length > 0 && !create.isPending;

  return (
    <Dialog
      open={mode !== null}
      onOpenChange={onOpenChange}
      title={mode === "group" ? "New group" : "New message"}
      size={mode === "group" ? "lg" : "md"}
      footer={
        mode === "group" ? (
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              disabled={!canCreate}
              loading={create.isPending}
              onClick={() => create.mutate({ kind: "group", title: title.trim(), member_ids: members.map((m) => m.id) }, { onSuccess: (d) => open(d.id) })}
            >
              Create group
            </Button>
          </>
        ) : undefined
      }
    >
      <div className="flex flex-col gap-4">
        {create.isError && <Banner tone="danger">{errorMessage(create.error)}</Banner>}
        {mode === "group" && (
          <>
            <Field label="Group name">
              <Input value={title} maxLength={80} onChange={(e) => setTitle(e.target.value)} placeholder="Weekend plans" />
            </Field>
            {members.length > 0 && (
              <ul className="flex flex-wrap gap-2" aria-label="Selected people">
                {members.map((m) => (
                  <li key={m.id}>
                    <Chip onClick={() => toggle(m)} icon={<X />} aria-label={`Remove ${m.display_name}`}>
                      {m.display_name}
                    </Chip>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        <PeoplePicker
          multiple={mode === "group"}
          selected={members}
          onToggle={toggle}
          disabled={create.isPending}
          onPick={(u) => create.mutate({ kind: "direct", user_id: u.id }, { onSuccess: (d) => open(d.id) })}
        />
      </div>
    </Dialog>
  );
}
