"use client";

import { errorMessage, useCrmChangeForm, useCrmForms } from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, Input, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { ChevronRight, ClipboardList, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { formatRelative } from "@/lib/format";
import { CrmFrame, useCrm } from "./CrmFrame";

/** /crm/forms: lead forms people fill in on a public page or an embed; each submission creates or updates a contact. */
export function CrmFormsScreen() {
  const [creating, setCreating] = useState(false);
  return (
    <CrmFrame actions={<NewForm onClick={() => setCreating(true)} />}>
      <Forms />
      {creating ? <CreateForm onClose={() => setCreating(false)} /> : null}
    </CrmFrame>
  );
}

function NewForm({ onClick }: { onClick: () => void }) {
  const { settings } = useCrm();
  if (!settings.can.manage_forms) return null;
  return (
    <Button size="auto" leftIcon={<Plus />} onClick={onClick}>
      New form
    </Button>
  );
}

function Forms() {
  const { orgId } = useCrm();
  const list = useCrmForms(orgId);
  return (
    <section className="flex flex-col gap-4" aria-label="Forms">
      <h2 className="type-title-sm text-fg">Forms</h2>
      {list.isPending ? (
        <Card padding="none" className="p-1">
          <SkeletonGroup label="Loading forms">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState title="Couldn't load forms" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
      ) : list.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState icon={<ClipboardList />} title="No forms yet" info="A form collects leads on a public page or an embed on your website. Each submission creates a contact, or updates the one with the same email." />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {list.data.items.map((f) => (
              <li key={f.id}>
                <Link href={`/crm/forms/${f.id}`} className="flex items-center gap-3 px-4 py-3 hover:bg-sunken">
                  <ClipboardList className="size-5 shrink-0 text-fg-muted" aria-hidden />
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="flex flex-wrap items-center gap-2">
                      <span className="truncate type-label font-semibold text-fg">{f.name}</span>
                      {f.published ? (
                        <Badge tone="success" dot>
                          Published
                        </Badge>
                      ) : (
                        <Badge>Not published</Badge>
                      )}
                    </span>
                    <span className="truncate type-caption text-fg-muted">
                      {f.submission_count === 1 ? "1 submission" : `${f.submission_count} submissions`} · {f.fields.length} fields · changed {formatRelative(f.updated_at)}
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

function CreateForm({ onClose }: { onClose: () => void }) {
  const { orgId } = useCrm();
  const change = useCrmChangeForm(orgId);
  const router = useRouter();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (!name.trim()) return setError("Name the form.");
    change.mutate(
      { op: "save", body: { name: name.trim() } },
      {
        onSuccess: (f) => {
          if (f) router.push(`/crm/forms/${f.id}`);
          toast.success("Form made with first name, last name and email");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't make the form.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="New form"
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={change.isPending} onClick={submit}>
            Make form
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" required error={error}>
          <Input value={name} maxLength={120} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Book a demo" />
        </Field>
      </form>
    </Dialog>
  );
}
