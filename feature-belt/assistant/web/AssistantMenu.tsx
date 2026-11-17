"use client";

import { assistantContextPrompt, useAssistantContexts, useAssistantStatus } from "@scaffoldapp/shared";
import { MessageCircleQuestion, Sparkles } from "lucide-react";
import type { ResourceMenuAction, ResourceMenuContext } from "../registry";
import { openAssistantPanel } from "./store";

/**
 * Every ⋮ menu on a resource the assistant can read: "Summarise" (with a submenu of scopes
 * for kinds that have them, like a channel's unread messages or last 7 days) and "Ask about
 * this". Both open the side panel with the resource attached. Nothing shows while the
 * assistant is off, the kind has no reader, or the person's AI agent scope keeps it out.
 */
export function useAssistantMenuActions(ctx: ResourceMenuContext): { actions: ResourceMenuAction[] } {
  const status = useAssistantStatus({ enabled: !!ctx.kind });
  const contexts = useAssistantContexts({ enabled: !!ctx.kind });
  const kind = ctx.kind ? contexts.data?.items.find((k) => k.kind === ctx.kind) : undefined;
  if (!kind || !ctx.id || !status.data?.available) return { actions: [] };
  const id = ctx.id;
  const context = { kind: kind.kind, id, ...(ctx.title ? { title: ctx.title } : {}) };
  const summarise = (scope?: string) =>
    openAssistantPanel({
      context: { ...context, ...(scope ? { scope } : {}) },
      mode: "summarise",
      prompt: assistantContextPrompt(kind, scope, ctx.title),
      label: kind.label,
    });
  const actions: ResourceMenuAction[] = [
    kind.scopes.length > 1
      ? {
          id: "assistant.summarise",
          label: "Summarise",
          icon: Sparkles,
          choices: kind.scopes.map((s) => ({ id: s.id, label: s.label, onSelect: () => summarise(s.id) })),
        }
      : { id: "assistant.summarise", label: "Summarise", icon: Sparkles, onSelect: () => summarise(kind.scopes[0]?.id) },
    {
      id: "assistant.ask",
      label: "Ask about this",
      icon: MessageCircleQuestion,
      onSelect: () => openAssistantPanel({ context, mode: "ask", label: kind.label }),
    },
  ];
  return { actions };
}
