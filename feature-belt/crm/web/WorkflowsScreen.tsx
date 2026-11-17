"use client";

import { CRM_TYPE_LABEL, errorMessage, useCrmChangeWorkflow, useCrmWorkflows, type CrmWorkflow } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, SkeletonGroup, SkeletonRow, Switch, toast } from "@scaffoldapp/ui-web";
import { ChevronRight, Plus, Workflow } from "lucide-react";
import Link from "next/link";
import { CrmFrame, useCrm } from "./CrmFrame";

/** One line describing what starts a workflow. */
export function triggerText(w: Pick<CrmWorkflow, "trigger" | "object_type">): string {
  const one = CRM_TYPE_LABEL[w.object_type].one;
  switch (w.trigger.type) {
    case "created":
      return `When a ${one} is created`;
    case "property_changed":
      return `When a ${one}'s ${(w.trigger.property ?? "").replace(/_/g, " ")} changes`;
    case "stage_changed":
      return w.trigger.stage_id ? `When a ${one} enters a stage` : `When a ${one} changes stage`;
    default: {
      const d = w.trigger.offset_days ?? 0;
      const when = d === 0 ? "on" : d > 0 ? `${d} days after` : `${-d} days before`;
      return `${when[0]?.toUpperCase()}${when.slice(1)} a ${one}'s ${(w.trigger.property ?? "").replace(/_/g, " ")}`;
    }
  }
}

/** /crm/workflows: automations that run on records as the person who turned them on. */
export function CrmWorkflowsScreen() {
  return (
    <CrmFrame actions={<NewWorkflow />}>
      <Workflows />
    </CrmFrame>
  );
}

function NewWorkflow() {
  const { settings } = useCrm();
  if (!settings.can.manage_workflows) return null;
  return (
    <Button asChild size="auto" leftIcon={<Plus />}>
      <Link href="/crm/workflows/new">New workflow</Link>
    </Button>
  );
}

function Workflows() {
  const { orgId } = useCrm();
  const list = useCrmWorkflows(orgId);
  const change = useCrmChangeWorkflow(orgId);
  return (
    <section className="flex flex-col gap-4" aria-label="Workflows">
      <h2 className="type-title-sm text-fg">Workflows</h2>
      {list.isPending ? (
        <Card padding="none" className="p-1">
          <SkeletonGroup label="Loading workflows">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState title="Couldn't load workflows" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
      ) : list.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState
            icon={<Workflow />}
            title="No workflows yet"
            info="A workflow reacts to records — created, a property changed, a stage entered, a date reached — and runs steps: set a property, make a task, rotate the owner, notify people, tag, create a deal or ticket, call a webhook, with waits in between."
          />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {list.data.items.map((w) => (
              <li key={w.id} className="flex items-center gap-3 px-4 py-3">
                <Switch
                  aria-label={w.enabled ? `Turn off ${w.name}` : `Turn on ${w.name}`}
                  checked={w.enabled}
                  disabled={!list.data.can_manage || change.isPending}
                  onCheckedChange={(on) =>
                    change.mutate(
                      { op: "save", id: w.id, body: { enabled: on } },
                      { onSuccess: () => toast.success(on ? "On: it runs as you" : "Off: waiting runs stopped"), onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) },
                    )
                  }
                />
                <Link href={`/crm/workflows/${w.id}`} className="flex min-w-0 flex-1 items-center gap-3 rounded-md hover:bg-sunken">
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="truncate type-label font-semibold text-fg">{w.name}</span>
                      <Badge>{CRM_TYPE_LABEL[w.object_type].Many}</Badge>
                      {w.stats && w.stats.failed > 0 ? <Badge tone="danger">{w.stats.failed} failed</Badge> : null}
                    </span>
                    <span className="truncate type-caption text-fg-muted">
                      {triggerText(w)} · {w.actions.length === 1 ? "1 step" : `${w.actions.length} steps`} {w.stats ? ` · ${w.stats.enrolled} enrolled` : ""}
                      {w.enabled && w.owner ? ` · runs as ${w.owner.name}` : ""}
                    </span>
                  </span>
                  <ChevronRight className="size-4 text-fg-subtle" aria-hidden />
                </Link>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </section>
  );
}
