"use client";

import { DEV_IDENTITIES, errorMessage, useAuthSession, useDevAuth } from "@scaffoldapp/shared";
import { Badge, Button, Card, Field, InfoTip, Input, ListGroup, ListRow, Spinner, toast } from "@scaffoldapp/ui-web";
import { useState, type FormEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { APP_NAME } from "@/lib/app";

/** `dev:<subject>:<email>` subject for a typed email: stable, so the same email is the same account. */
function subjectFor(email: string) {
  return email.toLowerCase().replace(/[^a-z0-9]+/g, "_");
}

/**
 * Development sign-in (no Clerk key, NEXT_PUBLIC_AUTH_DEV=true, non-production), laid out
 * like Clerk's card so both modes read as one product: an email, then the seeded accounts.
 * The backend accepts these tokens only with AUTH_DEV_BYPASS=true and APP_ENV=development.
 */
export function DevSignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const dev = useDevAuth();
  const auth = useAuthSession();
  const [busy, setBusy] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState<string | null>(null);

  const signIn = async (identity: { subject: string; email: string; name: string }) => {
    if (busy) return;
    if (!dev) {
      toast.error("Development sign-in isn't available in this build.");
      return;
    }
    setBusy(identity.subject);
    try {
      if (auth.isSignedIn && auth.userId !== identity.subject) await auth.signOut();
      await dev.signInAs(identity);
      onSignedIn();
    } catch (e) {
      toast.error(errorMessage(e, `Couldn't sign in as ${identity.name}.`));
      setBusy(null);
    }
  };

  const submitEmail = (e: FormEvent) => {
    e.preventDefault();
    const value = email.trim().toLowerCase();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
      setEmailError("Enter an email address.");
      return;
    }
    setEmailError(null);
    void signIn({ subject: subjectFor(value), email: value, name: value.split("@")[0] ?? value });
  };

  return (
    <Card className="flex flex-col gap-subsection">
      <h1 className="text-center type-title text-fg">Sign in to {APP_NAME}</h1>
      <form className="flex flex-col gap-3" onSubmit={submitEmail} noValidate>
        <Field label="Email address" error={emailError ?? undefined}>
          <Input type="email" inputMode="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@work-email.com" />
        </Field>
        <Button type="submit" fullWidth loading={busy !== null && busy === subjectFor(email.trim())}>
          Continue
        </Button>
      </form>
      <div aria-hidden className="flex items-center gap-3 type-caption text-fg-muted">
        <span className="h-px flex-1 bg-border" />
        or
        <span className="h-px flex-1 bg-border" />
      </div>
      <section aria-labelledby="dev-accounts" className="flex flex-col gap-2">
        <div className="flex items-center gap-1">
          <h2 id="dev-accounts" className="type-label text-fg-muted">
            Development accounts
          </h2>
          <InfoTip
            label="About development sign-in"
            content="Builds without a Clerk key and with NEXT_PUBLIC_AUTH_DEV=true sign in without a password: pick a seeded account, or type any email (a new address creates its account). Production builds always use Clerk."
          />
        </div>
        <ListGroup>
          <ul className="flex flex-col">
            {DEV_IDENTITIES.map((identity) => (
              <li key={identity.subject}>
                <ListRow
                  leading={<AppAvatar name={identity.name} size="sm" />}
                  title={
                    <>
                      {identity.name}
                      <span className="ml-2 hidden type-label font-normal text-fg-muted sm:inline">{identity.email}</span>
                    </>
                  }
                  trailing={
                    busy === identity.subject ? (
                      <Spinner size={16} />
                    ) : identity.admin_role ? (
                      <Badge tone="primary">{identity.admin_role === "admin" ? "Admin" : "Support"}</Badge>
                    ) : undefined
                  }
                  aria-label={`Sign in as ${identity.name}`}
                  aria-busy={busy === identity.subject || undefined}
                  onClick={() => void signIn({ subject: identity.subject, email: identity.email, name: identity.name })}
                />
              </li>
            ))}
          </ul>
        </ListGroup>
      </section>
    </Card>
  );
}
