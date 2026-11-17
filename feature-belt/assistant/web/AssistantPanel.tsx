"use client";

import { Button, Sheet } from "@scaffoldapp/ui-web";
import { Maximize2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { openAssistantPanel, useAssistantPanel } from "./store";
import { Thread } from "./Thread";

/**
 * The assistant beside the page (registry `sessionMounts`): a ⋮ menu's Summarise or Ask about
 * this opens it with the resource attached, so the person stays where they are. A side panel
 * from 768 px, a bottom sheet on phones; "Open in full" continues in /assistant/<id>.
 */
export function AssistantPanel() {
  const state = useAssistantPanel();
  const pathname = usePathname();
  const [conversationId, setConversationId] = useState<string | undefined>(undefined);
  useEffect(() => setConversationId(undefined), [state?.key]);
  // Leaving for the full assistant closes the panel.
  useEffect(() => {
    if (pathname?.startsWith("/assistant")) openAssistantPanel(null);
  }, [pathname]);

  return (
    <Sheet
      open={!!state}
      onOpenChange={(o) => !o && openAssistantPanel(null)}
      title="Assistant"
      size="lg"
      footer={
        conversationId ? (
          <Button asChild variant="ghost" size="sm" leftIcon={<Maximize2 />}>
            <Link href={`/assistant/${conversationId}`}>Open in full</Link>
          </Button>
        ) : undefined
      }
    >
      {state ? (
        <div className="flex h-[min(70dvh,48rem)] min-h-0 flex-col md:h-[calc(100dvh-10rem)]">
          <Thread key={state.key} conversationId={conversationId} onCreated={setConversationId} launch={state.launch} variant="panel" />
        </div>
      ) : null}
    </Sheet>
  );
}
