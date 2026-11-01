"use client";

import {
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
import { toast } from "@scaffoldapp/ui-web";
import { Bot } from "lucide-react";
import type { ResourceMenuAction, ResourceMenuChoice, ResourceMenuContext } from "../registry";

/**
 * What a ⋮ menu's scope is about: an external service ("connectors:<id>") is set as a
 * whole even on its connection's menu; otherwise one item (kind + id) or the feature.
 */
export function agentsMenuRef(ctx: ResourceMenuContext): AgentsScopeRef | null {
  if (!ctx.feature) return null;
  if (ctx.feature.startsWith("connectors:")) return { feature: ctx.feature };
  if (ctx.kind && ctx.id) return { feature: ctx.feature, kind: ctx.kind, id: ctx.id };
  return ctx.kind || ctx.id ? null : { feature: ctx.feature };
}

/**
 * The ⋮ menu's "AI agent access ▸ Default (Read) · Off · Read · Suggest · Edit" for a
 * whole feature (ctx.feature), one item (kind + id) or an external service
 * (feature "connectors:<id>"). Menus on one screen share one request.
 */
export function useAgentsMenuActions(ctx: ResourceMenuContext): { actions: ResourceMenuAction[] } {
  const ref = agentsMenuRef(ctx);
  const scope = useAgentsScope(ref);
  const set = useAgentsSetScope();
  if (!ref) return { actions: [] };
  const info = scope.data;
  if (!info) {
    return {
      actions: [{ id: "agents", label: "AI agent access", icon: Bot, hint: scope.isError ? "Couldn't load" : "Loading", disabled: true }],
    };
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
      hint: agentsInheritedSource(info),
      disabled: set.isPending,
      onSelect: () => choose(null),
    },
    ...AGENTS_LEVELS.map((level) => {
      const capped = agentsCappedBy(info, level);
      return {
        id: level,
        label: AGENTS_LEVEL_LABEL[level],
        checked: info.override === level,
        ...(capped ? { hint: `Capped at ${info.my_level}` } : {}),
        disabled: set.isPending,
        onSelect: () => choose(level),
      };
    }),
  ];
  return { actions: [{ id: "agents", label: "AI agent access", icon: Bot, hint: agentsScopeHint(info), choices }] };
}
