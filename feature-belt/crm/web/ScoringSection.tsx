"use client";

import { crmFilterReady, errorMessage, useCrmChangeScoreRule, useCrmScoreRules, type CrmFilter, type CrmScoreRule } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, MenuSeparator, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Gauge, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Section } from "@/components/shell/PageHeader";
import { useCrm } from "./CrmFrame";
import { ConditionsEditor, useConditionText } from "./Conditions";

/** Settings → Lead scoring: rules of contact conditions and points; a contact's score is the sum of the rules it matches. */
export function ScoringSection({ can }: { can: boolean }) {
  const { orgId } = useCrm();
  const rules = useCrmScoreRules(orgId);
  const change = useCrmChangeScoreRule(orgId);
  const text = useConditionText("contact");
  const [editing, setEditing] = useState<CrmScoreRule | "new" | null>(null);
  const [deleting, setDeleting] = useState<CrmScoreRule | null>(null);
  return (
    <Section
      title="Lead scoring"
      info="A contact's lead score is the sum of the points of every rule it matches. Scores update when a contact changes and every few hours, for rules on dates."
      action={
        can ? (
          <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New rule
          </Button>
        ) : undefined
      }
    >
      <Card padding="none" className="p-1">
        {rules.isPending ? (
          <SkeletonGroup label="Loading scoring rules">
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        ) : rules.isError ? (
          <ErrorState compact title="Couldn't load scoring rules" onRetry={() => void rules.refetch()} />
        ) : rules.data.items.length === 0 ? (
          <EmptyState compact icon={<Gauge />} title="No scoring rules" />
        ) : (
          <ul className="flex flex-col">
            {rules.data.items.map((r) => (
              <li key={r.id} className="flex items-center gap-3 rounded-md px-3 py-2 hover:bg-sunken">
                <Badge tone={r.points > 0 ? "success" : "danger"} className="tabular-nums">
                  {r.points > 0 ? `+${r.points}` : r.points}
                </Badge>
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate type-label font-semibold text-fg">{r.label}</span>
                  <span className="truncate type-caption text-fg-muted">{text(r.filters)}</span>
                </span>
                {can ? (
                  <Menu trigger={<IconButton label={`Actions for ${r.label}`} size="sm" icon={<MoreHorizontal />} />}>
                    <MenuItem icon={<Pencil />} onSelect={() => setEditing(r)}>
                      Edit
                    </MenuItem>
                    <MenuSeparator />
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(r)}>
                      Delete
                    </MenuItem>
                  </Menu>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </Card>
      {editing ? <RuleDialog rule={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={`Delete “${deleting?.label ?? ""}”?`}
        confirmLabel="Delete rule"
        onConfirm={() =>
          deleting
            ? change.mutateAsync({ op: "delete", id: deleting.id }).then(
                () => toast.success("Rule deleted; scores update in a moment"),
                (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
              )
            : undefined
        }
      >
        Contacts lose its points when scores update.
      </ConfirmDialog>
    </Section>
  );
}

function RuleDialog({ rule, onClose }: { rule: CrmScoreRule | null; onClose: () => void }) {
  const { orgId } = useCrm();
  const change = useCrmChangeScoreRule(orgId);
  const [label, setLabel] = useState(rule?.label ?? "");
  const [points, setPoints] = useState(rule ? String(rule.points) : "10");
  const [filters, setFilters] = useState<CrmFilter[]>(rule?.filters ?? []);
  const [error, setError] = useState<string | null>(null);
  const save = () => {
    const n = Number(points);
    if (!label.trim()) return setError("Name the rule.");
    if (!Number.isInteger(n) || n === 0 || n < -1000 || n > 1000) return setError("Points are a whole number from -1,000 to 1,000, not 0.");
    const ready = filters.filter(crmFilterReady);
    if (ready.length === 0) return setError("Add at least one condition.");
    setError(null);
    change.mutate(
      { op: "save", ...(rule ? { id: rule.id } : {}), body: { label: label.trim(), points: n, filters: ready } },
      {
        onSuccess: () => {
          toast.success("Rule saved; scores update in a moment");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the rule.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={rule ? "Edit scoring rule" : "New scoring rule"}
      info="Contacts matching every condition get the points. Use negative points for signals that lower a score."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={change.isPending} onClick={save}>
            Save rule
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <Field label="Name" required error={error}>
            <Input value={label} maxLength={120} autoFocus onChange={(e) => setLabel(e.target.value)} placeholder="Decision maker" />
          </Field>
          <Field label="Points">
            <Input value={points} inputMode="numeric" onChange={(e) => setPoints(e.target.value)} />
          </Field>
        </div>
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-2 type-label font-semibold text-fg">Conditions</legend>
          <ConditionsEditor type="contact" filters={filters} onChange={setFilters} noMe emptyText="Add the conditions a contact must match." />
        </fieldset>
      </div>
    </Dialog>
  );
}
