"use client";

import { errorMessage, hasErrorCode, useAgentsAccept, useAgentsSummary, useMarkNotificationsRead, useRealtimeEvent } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-web";
import { usePathname, useRouter } from "next/navigation";

interface SuggestionNotification {
  id?: string;
  kind: string;
  title: string;
  link?: string;
  data?: { suggestion_id?: string };
}

/**
 * Registry `sessionMounts`: a live toast the moment an agent suggests a change, with
 * Review and Accept, so someone working next to their agent can answer without leaving
 * the screen. It takes over the notifications feature's toast for the same notification
 * (same toast id) and keeps the waiting count loaded for badges. Renders nothing.
 */
export function AgentsSuggestionToasts(): null {
  useAgentsSummary();
  const router = useRouter();
  const pathname = usePathname();
  const accept = useAgentsAccept();
  const markRead = useMarkNotificationsRead();

  useRealtimeEvent("notification", (e) => {
    const n = e.data as unknown as SuggestionNotification;
    const id = n.data?.suggestion_id;
    if (n.kind !== "agents.suggestion" || !id) return;
    const toastId = n.id ?? `agents-suggestion-${id}`;
    const link = `/agents/suggestions/${id}`;
    // After every other handler for this event, so this toast replaces the plain one.
    setTimeout(() => {
      if (document.visibilityState !== "visible" || pathname === "/agents/suggestions" || pathname === link) {
        toast.dismiss(toastId);
        return;
      }
      const read = () => {
        if (n.id) markRead.mutate({ ids: [n.id] });
      };
      toast(n.title, {
        id: toastId,
        duration: 12_000,
        cancel: {
          label: "Review",
          onClick: () => {
            read();
            router.push(link);
          },
        },
        action: {
          label: "Accept",
          onClick: () => {
            read();
            accept.mutate(
              { id },
              {
                onSuccess: (s) => {
                  if (s.status === "accepted") toast.success("Done");
                  else toast.error(s.error?.message ?? "Couldn't apply this change.", { action: { label: "Review", onClick: () => router.push(link) } });
                },
                onError: (err) => {
                  if (hasErrorCode(err, "target_changed")) {
                    toast.warning("This changed since it was suggested. Review it first.", { action: { label: "Review", onClick: () => router.push(link) } });
                  } else toast.error(errorMessage(err, "Couldn't accept. Try again."));
                },
              },
            );
          },
        },
      });
    }, 0);
  });
  return null;
}
