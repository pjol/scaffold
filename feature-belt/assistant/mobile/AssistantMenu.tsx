import { assistantContextPrompt, assistantContextQuery, useAssistantContexts, useAssistantStatus, type AssistantContextLaunch } from "@scaffoldapp/shared";
import { router, type Href } from "expo-router";
import { MessageCircleQuestion, Sparkles } from "lucide-react-native";
import type { ResourceMenuAction, ResourceMenuContext } from "../registry";

function open(l: AssistantContextLaunch) {
  router.push(`/assistant/new?${assistantContextQuery(l)}` as Href);
}

/**
 * Every ⋮ sheet on a resource the assistant can read: "Summarise" (a second sheet of scopes
 * for kinds that have them) and "Ask about this", each opening a new conversation with the
 * resource attached. Nothing shows while the assistant is off, the kind has no reader, or the
 * person's AI agent scope keeps it out.
 */
export function useAssistantMenuActions(ctx: ResourceMenuContext): { actions: ResourceMenuAction[] } {
  const status = useAssistantStatus({ enabled: !!ctx.kind });
  const contexts = useAssistantContexts({ enabled: !!ctx.kind });
  const kind = ctx.kind ? contexts.data?.items.find((k) => k.kind === ctx.kind) : undefined;
  if (!kind || !ctx.id || !status.data?.available) return { actions: [] };
  const context = { kind: kind.kind, id: ctx.id, ...(ctx.title ? { title: ctx.title } : {}) };
  const summarise = (scope?: string) =>
    open({ context: { ...context, ...(scope ? { scope } : {}) }, mode: "summarise", prompt: assistantContextPrompt(kind, scope, ctx.title), label: kind.label });
  const actions: ResourceMenuAction[] = [
    kind.scopes.length > 1
      ? { id: "assistant.summarise", label: "Summarise", icon: Sparkles, choices: kind.scopes.map((s) => ({ id: s.id, label: s.label, onSelect: () => summarise(s.id) })) }
      : { id: "assistant.summarise", label: "Summarise", icon: Sparkles, onSelect: () => summarise(kind.scopes[0]?.id) },
    { id: "assistant.ask", label: "Ask about this", icon: MessageCircleQuestion, onSelect: () => open({ context, mode: "ask", label: kind.label }) },
  ];
  return { actions };
}
