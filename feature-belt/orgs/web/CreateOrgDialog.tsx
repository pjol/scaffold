"use client";

import { errorMessage, hasErrorCode, orgsNoun, setActiveOrgId, useCreateOrg } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, toast } from "@scaffoldapp/ui-web";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

const SLUG = /^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$/;

/** New organization: name and an optional URL name. Opens the new org's settings. */
export function CreateOrgDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const router = useRouter();
  const create = useCreateOrg();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [errors, setErrors] = useState<{ name?: string; slug?: string }>({});

  useEffect(() => {
    if (!open) return;
    setName("");
    setSlug("");
    setErrors({});
  }, [open]);

  const submit = async () => {
    const n = name.trim();
    const s = slug.trim().toLowerCase();
    const next: typeof errors = {};
    if (!n) next.name = "Enter a name.";
    else if (n.length > 80) next.name = "Use 80 characters or fewer.";
    if (s && (!SLUG.test(s) || s.includes("--"))) next.slug = "Use 3 to 40 lowercase letters, numbers or single hyphens.";
    setErrors(next);
    if (next.name || next.slug) return;
    try {
      const org = await create.mutateAsync({ name: n, ...(s ? { slug: s } : {}) });
      setActiveOrgId(org.id);
      toast.success(`${org.name} created`);
      onOpenChange(false);
      router.push(`/settings/orgs/${org.id}`);
    } catch (e) {
      if (hasErrorCode(e, "conflict")) setErrors({ slug: errorMessage(e) });
      else toast.error(errorMessage(e, `Couldn't create the ${orgsNoun()}. Try again.`));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !create.isPending && onOpenChange(o)}
      title={`New ${orgsNoun()}`}
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={create.isPending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={create.isPending} onClick={() => void submit()}>
            Create
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Name" error={errors.name} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} autoFocus />
        </Field>
        <Field label="URL name" info="Lowercase letters, numbers and hyphens. Left empty, one is made from the name." error={errors.slug}>
          <Input value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase())} maxLength={40} placeholder="riverside-climbing" autoCapitalize="none" spellCheck={false} />
        </Field>
      </form>
    </Dialog>
  );
}
