import {
  AGENTS_LEVEL_INFO,
  AGENTS_LEVEL_LABEL,
  AGENTS_LEVELS,
  agentsCappedBy,
  agentsInheritedSource,
  agentsScopeHint,
  errorMessage,
  useAgentsScope,
  useAgentsSetScope,
  type AgentsLevel,
  type AgentsScopeRef,
} from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import { Bot } from "lucide-react-native";
import type { ResourceMenuAction, ResourceMenuChoice, ResourceMenuContext } from "../registry";

/** An external service ("connectors:<id>") is set as a whole; otherwise one item (kind + id) or the feature. */
function menuRef(ctx: ResourceMenuContext): AgentsScopeRef | null {
  if (!ctx.feature) return null;
  if (ctx.feature.startsWith("connectors:")) return { feature: ctx.feature };
  if (ctx.kind && ctx.id) return { feature: ctx.feature, kind: ctx.kind, id: ctx.id };
  return ctx.kind || ctx.id ? null : { feature: ctx.feature };
}

/**
 * The ⋮ sheet's "AI agent access" row: a second sheet with Default (what it inherits),
 * Off, Read, Suggest and Edit, each explained in an InfoTip. Sheets on one screen share
 * one request.
 */
export function useAgentsMenuActions(ctx: ResourceMenuContext): { actions: ResourceMenuAction[] } {
  const ref = menuRef(ctx);
  const scope = useAgentsScope(ref);
  const set = useAgentsSetScope();
  if (!ref) return { actions: [] };
  const info = scope.data;
  if (!info) {
    return { actions: [{ id: "agents", label: "AI agent access", icon: Bot, hint: scope.isError ? "Couldn't load" : "Loading", disabled: true }] };
  }
  const choose = (level: AgentsLevel | null) => {
    if (level === info.override) return;
    set.mutate(
      { ...ref, level, ...(ctx.title ? { title: ctx.title } : {}) },
      {
        onSuccess: (next) =>
          toast.success(next.override ? `AI agents: ${AGENTS_LEVEL_LABEL[next.override]}` : `AI agents: ${AGENTS_LEVEL_LABEL[next.inherited.level]} (default)`),
        onError: (e) => toast.error(errorMessage(e, "Couldn't change AI agent access. Try again.")),
      },
    );
  };
  const choices: ResourceMenuChoice[] = [
    {
      id: "default",
      label: `Default (${AGENTS_LEVEL_LABEL[info.inherited.level]})`,
      checked: info.override === null,
      hint: `Follows ${agentsInheritedSource(info)}.`,
      onSelect: () => choose(null),
    },
    ...AGENTS_LEVELS.map((level) => ({
      id: level,
      label: AGENTS_LEVEL_LABEL[level],
      checked: info.override === level,
      hint: agentsCappedBy(info, level) ?? AGENTS_LEVEL_INFO[level],
      onSelect: () => choose(level),
    })),
  ];
  return { actions: [{ id: "agents", label: "AI agent access", icon: Bot, hint: agentsScopeHint(info), choices }] };
}
