"use client";

import { useAuthSession, useConfig } from "@scaffoldapp/shared";
import { Banner, Button, Card, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { LogIn, Trash2 } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { DeleteAccountDialog } from "@/components/account/DeleteAccountDialog";
import { useSignOut } from "@/components/auth/useSignOut";
import { APP_NAME } from "@/lib/app";
import { signInHref } from "@/lib/redirect";

/**
 * /delete-account: the public deletion URL Google Play requires. Signed out: the steps,
 * what is deleted and kept, a sign-in button, and the support address for people who
 * can't sign in. Signed in: the same delete flow as Settings → Account.
 */
export function DeleteAccountScreen() {
  const auth = useAuthSession();
  const config = useConfig();
  const params = useSearchParams();
  const signOut = useSignOut();
  const [open, setOpen] = useState(false);
  const done = params.get("done") === "1";
  const support = config.data?.support_email;

  return (
    <div className="mx-auto flex w-full max-w-prose flex-col gap-6 px-gutter py-section">
      <h1 className="type-title text-fg lg:type-display">Delete your {APP_NAME} account</h1>
      <div className="flex flex-col gap-section">
        {done && <Banner tone="success">Your account was deleted.</Banner>}

        <section className="flex flex-col gap-3">
          <h2 className="type-title-sm text-fg">How</h2>
          <ol className="flex list-decimal flex-col gap-2 pl-5 type-body text-fg marker:text-fg-muted">
            <li>Sign in to {APP_NAME} on the web or in the app.</li>
            <li>Open Settings, then Account, then Delete account.</li>
            <li>Type DELETE and confirm. The account is deleted immediately.</li>
          </ol>
        </section>

        <section className="flex flex-col gap-3">
          <h2 className="type-title-sm text-fg">What happens</h2>
          <ul className="flex list-disc flex-col gap-2 pl-5 type-body text-fg marker:text-fg-muted">
            <li>Deleted immediately: your name, email address, profile photo and uploaded files. You can't sign in to the account again.</li>
            <li>Kept without your name or email: the record of when you accepted the Terms of Service and Privacy Policy, which the law requires as proof of consent.</li>
          </ul>
        </section>

        <Card className="flex flex-col items-start gap-4">
          {!auth.isLoaded ? (
            <SkeletonGroup label="Checking your session" className="flex w-full flex-col gap-3">
              <Skeleton className="h-6 w-2/3" />
              <Skeleton className="h-10 w-40" />
            </SkeletonGroup>
          ) : auth.isSignedIn ? (
            <>
              <p className="type-body text-fg">
                Signed in as <span className="font-semibold">{auth.email ?? "your account"}</span>
              </p>
              <Button variant="danger" size="auto" leftIcon={<Trash2 />} onClick={() => setOpen(true)}>
                Delete account
              </Button>
            </>
          ) : (
            <Button asChild size="auto" leftIcon={<LogIn />}>
              <Link href={signInHref("/delete-account")}>Sign in to delete your account</Link>
            </Button>
          )}
        </Card>

        {support && (
          <p className="type-body text-fg">
            Can&apos;t sign in? Email{" "}
            <a className="font-semibold text-primary-strong underline underline-offset-2" href={`mailto:${support}?subject=${encodeURIComponent("Delete my account")}`}>
              {support}
            </a>{" "}
            from the address on the account.
          </p>
        )}

      </div>
      <DeleteAccountDialog open={open} onOpenChange={setOpen} onDeleted={() => signOut("/delete-account?done=1")} />
    </div>
  );
}
