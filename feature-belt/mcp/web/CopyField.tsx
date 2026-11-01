"use client";

import { Field, IconButton, Input, toast } from "@scaffoldapp/ui-web";
import { Check, Copy } from "lucide-react";
import { useState } from "react";

/** A read-only field with a copy button: endpoints, one-time secrets. */
export function CopyField({ label, value, mono = false, className }: { label: string; value: string; mono?: boolean; className?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Field label={label} {...(className ? { className } : {})}>
      <Input
        readOnly
        value={value}
        className={mono ? "font-mono" : undefined}
        onFocus={(e) => e.currentTarget.select()}
        trailing={
          <IconButton
            label={copied ? "Copied" : `Copy ${label}`}
            size="sm"
            variant="ghost"
            icon={copied ? <Check /> : <Copy />}
            onClick={() => {
              void navigator.clipboard.writeText(value).then(
                () => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                },
                () => toast.error("Couldn't copy. Select the text instead."),
              );
            }}
          />
        }
      />
    </Field>
  );
}
