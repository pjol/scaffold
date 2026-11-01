"use client";

import { cn } from "@scaffoldapp/ui-web";
import { useEffect, useRef, useState } from "react";
import { ConversationList } from "./ConversationList";
import { Thread } from "./Thread";

/**
 * /assistant, /assistant/new and /assistant/<id>. At ≥ 1024 px the conversation list sits
 * beside the thread; on phones /assistant is the list and the other two are the thread.
 * The page fills the viewport (below the top bar, above the phone tab bar) so the composer
 * stays in view and only the messages scroll.
 */
export function AssistantScreen({ conversationId }: { conversationId?: string }) {
  const routeId = conversationId === "new" ? undefined : conversationId;
  const [activeId, setActiveId] = useState(routeId);
  const [threadKey, setThreadKey] = useState(routeId ?? "new");
  const activeRef = useRef(activeId);
  activeRef.current = activeId;
  // Another conversation picked: a fresh thread. The URL catching up with a conversation
  // this thread just created (replaceState below) changes nothing.
  useEffect(() => {
    if (routeId === activeRef.current) return;
    setActiveId(routeId);
    setThreadKey(routeId ?? `new-${Date.now()}`);
  }, [routeId]);
  const threadOnPhone = conversationId !== undefined;

  return (
    <div className="mx-auto flex h-[calc(100dvh-var(--spacing-topbar)-4rem-env(safe-area-inset-bottom))] w-full max-w-content gap-8 px-gutter pt-4 lg:h-[calc(100dvh-var(--spacing-topbar))]">
      <ConversationList activeId={activeId} className={cn("w-full lg:w-72 lg:shrink-0 lg:border-r lg:border-border lg:pr-6", threadOnPhone && "hidden lg:flex")} />
      <section aria-label="Conversation" className={cn("min-w-0 flex-1 flex-col", threadOnPhone ? "flex" : "hidden lg:flex")}>
        <Thread
          key={threadKey}
          conversationId={activeId}
          onCreated={(id) => {
            setActiveId(id);
            // New address for the same thread; the stream keeps running.
            window.history.replaceState(null, "", `/assistant/${id}`);
          }}
        />
      </section>
    </div>
  );
}
