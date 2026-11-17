"use client";

import {
  errorMessage,
  useApplyForIdentityBadge,
  useResubmitIdentityBadgeApplication,
  type IdentityBadgeApplication,
  type IdentityBadges,
  type IdentityEvidenceKind,
} from "@scaffoldapp/shared";
import { Banner, Button, Chip, Dialog, Field, IconButton, Input, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type Draft = { kind: IdentityEvidenceKind; ref: string };

const KINDS: { value: IdentityEvidenceKind; label: string; placeholder: string }[] = [
  { value: "url", label: "Link", placeholder: "https://example.com/about" },
  { value: "domain", label: "Domain", placeholder: "example.com" },
  { value: "account", label: "Account", placeholder: "github:yourname" },
  { value: "text", label: "Note", placeholder: "Anything else a reviewer should know" },
];

const OPEN = new Set(["submitted", "needs_info"]);

/**
 * Apply for a reviewed badge, or (with `resubmit`) answer staff's request for more on an
 * application. Mount it keyed by the application so its fields start fresh.
 */
export function ApplyDialog({
  st,
  open,
  onOpenChange,
  resubmit,
}: {
  st: IdentityBadges;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  resubmit?: IdentityBadgeApplication | null;
}) {
  const apply = useApplyForIdentityBadge();
  const again = useResubmitIdentityBadgeApplication();
  const taken = new Set(st.applications.filter((a) => OPEN.has(a.status) && a.id !== resubmit?.id).map((a) => a.category));
  const [category, setCategory] = useState(resubmit?.category ?? st.categories.find((c) => !taken.has(c.key))?.key ?? "");
  const [label, setLabel] = useState(resubmit?.display_label ?? "");
  const [note, setNote] = useState(resubmit?.note ?? "");
  const [evidence, setEvidence] = useState<Draft[]>(resubmit ? resubmit.evidence.map(({ kind, ref }) => ({ kind, ref })) : [{ kind: "url", ref: "" }]);
  const [error, setError] = useState<string | null>(null);
  const cat = st.categories.find((c) => c.key === category);
  const filled = evidence.filter((e) => e.ref.trim());
  const verifiedDomains = st.domains.filter((d) => d.verified && !evidence.some((e) => e.kind === "domain" && e.ref === d.domain));
  const pending = apply.isPending || again.isPending;

  const edit = (i: number, next: Partial<Draft>) => setEvidence(evidence.map((e, j) => (j === i ? { ...e, ...next } : e)));
  const submit = () => {
    setError(null);
    const body = { category, display_label: label.trim(), note: note.trim(), evidence: filled.map((e) => ({ kind: e.kind, ref: e.ref.trim() })) };
    const done = {
      onSuccess: () => {
        toast.success(resubmit ? "Sent again" : "Application sent");
        onOpenChange(false);
      },
      onError: (e: unknown) => setError(errorMessage(e, "Couldn't send your application. Try again.")),
    };
    if (resubmit) again.mutate({ id: resubmit.id, body }, done);
    else apply.mutate(body, done);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={resubmit ? "Update your application" : "Apply for a badge"}
      info="Verified domains and linked accounts count as checked evidence. A reviewer opens links and reads notes."
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={pending} disabled={!category || filled.length === 0 || taken.has(category)} onClick={submit}>
            {resubmit ? "Send again" : "Send application"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        {resubmit?.reviewer_note ? <Banner tone="warning">{resubmit.reviewer_note}</Banner> : null}
        <Field label="Category" info={cat ? `Approval gives the ${st.kinds.find((k) => k.kind === cat.kind)?.label.toLowerCase() ?? cat.kind} badge.` : undefined}>
          <Select
            options={st.categories.map((c) => ({ value: c.key, label: taken.has(c.key) ? `${c.label} (already applied)` : c.label, disabled: taken.has(c.key) }))}
            value={category}
            onValueChange={setCategory}
            disabled={!!resubmit}
          />
        </Field>
        <Field label="Name to show" info="What the badge says: your business, organization or public name. Empty uses the category.">
          <Input value={label} maxLength={60} placeholder={cat?.label} onChange={(e) => setLabel(e.target.value)} />
        </Field>
        <Field label={`Evidence (${filled.length}/10)`} info="Links to pages that show who you are, domains you proved below, accounts on other platforms, notes.">
          <div className="flex flex-col gap-2">
            {evidence.map((ev, i) => (
              <div key={i} className="flex gap-2">
                <Select aria-label="Kind of evidence" className="w-32 shrink-0" options={KINDS.map((k) => ({ value: k.value, label: k.label }))} value={ev.kind} onValueChange={(v) => edit(i, { kind: v as IdentityEvidenceKind })} />
                <Input aria-label="Evidence" wrapperClassName="min-w-0 flex-1" value={ev.ref} maxLength={500} placeholder={KINDS.find((k) => k.value === ev.kind)?.placeholder} onChange={(e) => edit(i, { ref: e.target.value })} />
                <IconButton label="Remove" icon={<Trash2 />} variant="ghost" onClick={() => setEvidence(evidence.filter((_, j) => j !== i))} disabled={evidence.length === 1} />
              </div>
            ))}
            <div className="flex flex-wrap gap-2">
              {evidence.length < 10 ? (
                <Button size="sm" variant="secondary" leftIcon={<Plus />} onClick={() => setEvidence([...evidence, { kind: "url", ref: "" }])}>
                  Add evidence
                </Button>
              ) : null}
              {evidence.length < 10
                ? verifiedDomains.map((d) => (
                    <Chip key={d.domain} onClick={() => setEvidence([...evidence.filter((e) => e.ref.trim()), { kind: "domain", ref: d.domain }])}>
                      + {d.domain}
                    </Chip>
                  ))
                : null}
            </div>
          </div>
        </Field>
        <Field label="Note for reviewers">
          <TextArea value={note} maxLength={2000} minRows={3} onChange={(e) => setNote(e.target.value)} />
        </Field>
        {error ? <Banner tone="danger">{error}</Banner> : null}
      </div>
    </Dialog>
  );
}
