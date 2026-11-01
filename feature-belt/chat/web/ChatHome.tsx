"use client";

import { Button, EmptyState } from "@scaffoldapp/ui-web";
import { MessagesSquare } from "lucide-react";
import { useState } from "react";
import { NewConversationDialog, type NewConversationMode } from "./NewConversationDialog";

/** /chat beside the inbox at ≥ 1024 px (phones show the inbox list instead). */
export function ChatHome() {
  const [mode, setMode] = useState<NewConversationMode | null>(null);
  return (
    <div className="flex flex-1 items-center justify-center px-gutter py-section">
      <EmptyState
        icon={<MessagesSquare />}
        title="Choose a conversation"
        action={
          <Button variant="secondary" size="auto" onClick={() => setMode("direct")}>
            New message
          </Button>
        }
      />
      <NewConversationDialog mode={mode} onOpenChange={(open) => !open && setMode(null)} />
    </div>
  );
}
