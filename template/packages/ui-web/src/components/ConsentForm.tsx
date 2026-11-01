"use client";

import { ArrowRight, CircleAlert } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useState, type FormEvent, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { Button } from "./Button";
import { Checkbox } from "./Checkbox";
import { LegalDocument, type LegalDocumentData } from "./LegalDocument";
import { Sheet } from "./Sheet";
import { SkeletonText } from "./Skeleton";

export interface ConsentFormValue {
  termsVersion: string;
  privacyVersion: string;
}

export interface ConsentFormProps {
  /** Current Terms of Service (`null` while loading: the sheet shows a skeleton). */
  terms: LegalDocumentData | null | undefined;
  privacy: LegalDocumentData | null | undefined;
  /** Record consent (`useAcceptConsents().mutateAsync`). Throw to show the error and keep the form. */
  onSubmit: (value: ConsentFormValue) => void | Promise<unknown>;
  /** @default "Agree and continue" */
  submitLabel?: ReactNode;
  /** Message for a failed `onSubmit`. @default the error's message */
  errorText?: (error: unknown) => ReactNode;
  className?: string;
}

function DocLink({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onClick();
      }}
      className="relative inline font-semibold text-primary-strong underline underline-offset-2 before:absolute before:-inset-x-1 before:-inset-y-2 before:content-['']"
    >
      {children}
    </button>
  );
}

/**
 * Consent step: one required "I agree to the Terms of Service and Privacy Policy" box
 * (each name opens the full text in a sheet) and one submit button that stays disabled
 * until the box is checked.
 */
export function ConsentForm({ terms, privacy, onSubmit, submitLabel = "Agree and continue", errorText, className }: ConsentFormProps) {
  const t = useMotionTokens();
  const [agreed, setAgreed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ReactNode>(null);
  const [open, setOpen] = useState<"terms" | "privacy" | null>(null);
  const ready = !!terms && !!privacy;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!agreed || !terms || !privacy || busy) return;
    setError(null);
    setBusy(true);
    try {
      await onSubmit({ termsVersion: terms.version, privacyVersion: privacy.version });
    } catch (err) {
      setError(errorText ? errorText(err) : err instanceof Error && err.message ? err.message : "That didn't go through. Try again.");
    } finally {
      setBusy(false);
    }
  };

  const doc = open === "terms" ? terms : open === "privacy" ? privacy : null;

  return (
    <form onSubmit={(e) => void submit(e)} className={cn("flex flex-col gap-6", className)} noValidate>
      <div className="flex flex-col gap-3">
        <Checkbox
          checked={agreed}
          onCheckedChange={setAgreed}
          disabled={busy}
          required
          label={
            <>
              I agree to the <DocLink onClick={() => setOpen("terms")}>Terms of Service</DocLink> and <DocLink onClick={() => setOpen("privacy")}>Privacy Policy</DocLink>
            </>
          }
          info="Required to use the app. You'll be asked again when either document changes."
        />
        <AnimatePresence initial={false}>
          {error && (
            <motion.div key="error" role="alert" initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: "auto" }} exit={{ opacity: 0, height: 0 }} transition={t.spring.gentle} className="overflow-hidden">
              <div className="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 type-label text-danger-strong">
                <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden />
                <span>{error}</span>
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>
      <Button type="submit" size="lg" fullWidth disabled={!agreed || !ready} loading={busy} rightIcon={<ArrowRight />}>
        {submitLabel}
      </Button>
      <Sheet open={open !== null} onOpenChange={(o) => !o && setOpen(null)} title={doc?.title ?? (open === "terms" ? "Terms of Service" : "Privacy Policy")} size="lg">
        {doc ? (
          <LegalDocument document={doc} showTitle={false} toc={false} />
        ) : (
          <div className="flex flex-col gap-6" aria-busy>
            <SkeletonText lines={4} />
            <SkeletonText lines={6} />
          </div>
        )}
      </Sheet>
    </form>
  );
}
