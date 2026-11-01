"use client";

import {
  SUPPORT_BODY_MAX,
  SUPPORT_CATEGORIES,
  SUPPORT_CATEGORY_LABEL,
  SUPPORT_SUBJECT_MAX,
  errorMessage,
  hasErrorCode,
  useCreateTicket,
  type SupportCategory,
} from "@scaffoldapp/shared";
import { Banner, Button, Field, Input, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { Send } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Screen } from "@/components/shell/PageHeader";
import { AttachmentPicker, BackLink, attachmentInputs, attachmentsBusy, type PendingAttachment } from "./components";
import { supportPaths } from "./registry";

const CATEGORY_OPTIONS = SUPPORT_CATEGORIES.map((c) => ({ value: c, label: SUPPORT_CATEGORY_LABEL[c] }));

/** Settings → Support requests → New request: category, subject, message and up to 5 files. */
export function NewTicketScreen() {
  const router = useRouter();
  const create = useCreateTicket();
  const [category, setCategory] = useState<SupportCategory | "">("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [files, setFiles] = useState<PendingAttachment[]>([]);
  const [tried, setTried] = useState(false);

  const subjectLen = subject.trim().length;
  const errors = {
    category: category ? null : "Choose a category.",
    subject: subjectLen < 3 ? "Write at least 3 characters." : null,
    body: body.trim() ? null : "Describe what happened.",
  };
  const valid = !errors.category && !errors.subject && !errors.body;
  const busy = attachmentsBusy(files);

  return (
    <div className="flex flex-col gap-2">
      <BackLink href={supportPaths.list} label="Support requests" />
      <Screen title="New request" info="Include what you were doing and what you expected. Screenshots help.">
        <form
          noValidate
          className="flex max-w-form flex-col gap-6"
          onSubmit={(e) => {
            e.preventDefault();
            setTried(true);
            if (!valid || busy || !category) return;
            create.mutate(
              { category, subject: subject.trim(), body: body.trim(), attachments: attachmentInputs(files) },
              {
                onSuccess: (t) => {
                  toast.success("Request sent");
                  router.replace(supportPaths.ticket(t.id));
                },
              },
            );
          }}
        >
          {create.isError && (
            <Banner tone="danger">
              {hasErrorCode(create.error, "rate_limited") ? "You've sent several requests in the last hour. Try again later." : errorMessage(create.error, "Couldn't send your request. Try again.")}
            </Banner>
          )}
          <Field label="Category" required error={tried ? errors.category : undefined}>
            <Select options={CATEGORY_OPTIONS} value={category} onValueChange={(v) => setCategory(v as SupportCategory)} placeholder="Choose one" />
          </Field>
          <Field label="Subject" required error={tried ? errors.subject : undefined}>
            <Input value={subject} onChange={(e) => setSubject(e.target.value)} maxLength={SUPPORT_SUBJECT_MAX} autoComplete="off" />
          </Field>
          <Field label="Message" required error={tried ? errors.body : undefined}>
            <TextArea value={body} onChange={(e) => setBody(e.target.value)} maxLength={SUPPORT_BODY_MAX} minRows={5} maxRows={14} />
          </Field>
          <Field label="Attachments" info="Up to 5 files: photos, videos, PDFs or text files, 25 MB each.">
            <AttachmentPicker value={files} onChange={setFiles} disabled={create.isPending} />
          </Field>
          <Button type="submit" size="auto" leftIcon={<Send />} loading={create.isPending} disabled={busy} className="self-start">
            Send request
          </Button>
        </form>
      </Screen>
    </div>
  );
}
