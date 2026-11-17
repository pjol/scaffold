"use client";

import { errorMessage, useCrmChangeTemplate, useCrmTemplates, type CrmEmailTemplate } from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  SkeletonGroup,
  SkeletonRow,
  Switch,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { FileText, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useRef, useState } from "react";
import { formatRelative } from "@/lib/format";
import { CrmFrame, useCrm } from "./CrmFrame";
import { MergeFieldMenu, insertAtCaret, useCaretInsert } from "./MailParts";

/** /crm/templates: email templates with merge fields — yours, and the ones shared with everyone who uses the CRM. */
export function CrmTemplatesScreen() {
  const [editing, setEditing] = useState<CrmEmailTemplate | "new" | null>(null);
  return (
    <CrmFrame
      actions={
        <Button size="auto" leftIcon={<Plus />} onClick={() => setEditing("new")}>
          New template
        </Button>
      }
    >
      <Templates onEdit={setEditing} />
      {editing ? <TemplateDialog template={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
    </CrmFrame>
  );
}

function Templates({ onEdit }: { onEdit: (t: CrmEmailTemplate) => void }) {
  const { orgId } = useCrm();
  const list = useCrmTemplates(orgId);
  const change = useCrmChangeTemplate(orgId);
  const [deleting, setDeleting] = useState<CrmEmailTemplate | null>(null);
  return (
    <section className="flex flex-col gap-4" aria-label="Email templates">
      <h2 className="type-title-sm text-fg">Email templates</h2>
      {list.isPending ? (
        <Card padding="none" className="p-1">
          <SkeletonGroup label="Loading templates">
            <SkeletonRow leading={false} />
            <SkeletonRow leading={false} />
          </SkeletonGroup>
        </Card>
      ) : list.isError ? (
        <ErrorState title="Couldn't load templates" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isRefetching} />
      ) : list.data.items.length === 0 ? (
        <Card padding="compact">
          <EmptyState icon={<FileText />} title="No templates yet" info="Save emails you send often, with merge fields like {{contact.first_name}} that fill in for each contact." />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {list.data.items.map((t) => (
              <li key={t.id} className="flex items-center gap-3 px-4 py-3">
                <FileText className="size-5 shrink-0 text-fg-muted" aria-hidden />
                <button type="button" onClick={() => (t.can_edit ? onEdit(t) : undefined)} className="flex min-w-0 flex-1 flex-col text-left">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="truncate type-label font-semibold text-fg">{t.name}</span>
                    {t.shared ? <Badge tone="info">Shared</Badge> : null}
                  </span>
                  <span className="truncate type-caption text-fg-muted">
                    {t.subject || "No subject"} · {t.owner ? t.owner.name : "Former member"} · {formatRelative(t.updated_at)}
                  </span>
                </button>
                {t.can_edit ? (
                  <Menu trigger={<IconButton label={`Actions for ${t.name}`} size="sm" icon={<MoreHorizontal />} />}>
                    <MenuItem icon={<Pencil />} onSelect={() => onEdit(t)}>
                      Edit
                    </MenuItem>
                    <MenuSeparator />
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(t)}>
                      Delete
                    </MenuItem>
                  </Menu>
                ) : null}
              </li>
            ))}
          </ul>
        </Card>
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete template"
        onConfirm={() =>
          deleting
            ? change.mutateAsync({ op: "delete", id: deleting.id }).then(
                () => toast.success("Template deleted"),
                (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
              )
            : undefined
        }
      >
        Emails already sent with it stay as they are.
      </ConfirmDialog>
    </section>
  );
}

function TemplateDialog({ template, onClose }: { template: CrmEmailTemplate | null; onClose: () => void }) {
  const { orgId, settings } = useCrm();
  const list = useCrmTemplates(orgId);
  const change = useCrmChangeTemplate(orgId);
  const [name, setName] = useState(template?.name ?? "");
  const [subject, setSubject] = useState(template?.subject ?? "");
  const [body, setBody] = useState(template?.body ?? "");
  const [shared, setShared] = useState(template?.shared ?? false);
  const [error, setError] = useState<string | null>(null);
  const subjectRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const last = useRef<"subject" | "body">("body");
  const caret = useCaretInsert();
  const insert = (key: string) => {
    const token = `{{${key}}}`;
    if (last.current === "subject") {
      const r = insertAtCaret(subjectRef.current, subject, token);
      setSubject(r.value);
      caret.focusAfter(subjectRef.current, r.caret);
    } else {
      const r = insertAtCaret(bodyRef.current, body, token);
      setBody(r.value);
      caret.focusAfter(bodyRef.current, r.caret);
    }
  };
  const save = () => {
    if (!name.trim()) return setError("Name the template.");
    setError(null);
    change.mutate(
      { op: "save", ...(template ? { id: template.id } : {}), body: { name: name.trim(), subject, body, shared } },
      {
        onSuccess: () => {
          toast.success("Template saved");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the template.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={template ? "Edit template" : "New template"}
      info="Merge fields fill in from the contact, their company and the deal or ticket when you send. {{contact.first_name | there}} uses “there” when it's empty."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={change.isPending} onClick={save}>
            Save template
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name" required error={error}>
          <Input value={name} maxLength={120} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Follow-up after a demo" />
        </Field>
        <Field label="Subject">
          <Input ref={subjectRef} value={subject} maxLength={300} onFocus={() => (last.current = "subject")} onChange={(e) => setSubject(e.target.value)} />
        </Field>
        <Field label="Message" labelEnd={<MergeFieldMenu fields={list.data?.merge_fields} onInsert={insert} onTriggerFocus={caret.onTriggerFocus} />}>
          <TextArea ref={bodyRef} value={body} minRows={8} maxRows={18} maxLength={20000} onFocus={() => (last.current = "body")} onChange={(e) => setBody(e.target.value)} />
        </Field>
        <Switch
          checked={shared}
          onCheckedChange={setShared}
          disabled={!settings.can.manage_settings}
          label="Shared with everyone who uses the CRM"
          info={settings.can.manage_settings ? "Shared templates appear in everyone's composer." : "People who manage CRM settings can share templates."}
        />
      </div>
    </Dialog>
  );
}
