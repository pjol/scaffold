"use client";

import { DEV_IDENTITIES, errorMessage, useAuthSession, useDevAuth, type DevIdentity } from "@scaffoldapp/shared";
import { Badge, Button, Card, Field, InfoTip, Input, Spinner, Stagger, StaggerItem, toast } from "@scaffoldapp/ui-web";
import { FlaskConical } from "lucide-react";
import { useState, type FormEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";

/** `dev:<subject>:<email>` subject for a typed email: stable, so the same email is the same account. */
function subjectFor(email: string) {
  return email.toLowerCase().replace(/[^a-z0-9]+/g, "_");
}

function IdentityRow({ identity, busy, onPick }: { identity: DevIdentity; busy: boolean; onPick: () => void }) {
  return (
    <Card padding="none" className="relative flex min-h-tap-lg items-center gap-3 px-4 py-2 transition-colors hover:border-border-strong">
      <AppAvatar name={identity.name} size="md" />
      <span className="flex min-w-0 flex-1 items-center gap-2">
        {/* The button's ::after covers the card, so the whole row is the target; the ⓘ sits above it. */}
        <button
          type="button"
          onClick={onPick}
          disabled={busy}
          aria-label={`Sign in as ${identity.name}`}
          className="truncate text-left type-body font-semibold text-fg outline-none after:absolute after:inset-0 after:rounded-lg after:content-[''] focus-visible:after:outline-3 focus-visible:after:outline-ring disabled:cursor-wait"
        >
          {identity.name}
        </button>
        <span className="relative z-(--app-z-raised) inline-flex">
          <InfoTip content={`${identity.email}. ${identity.info}`} label={`About ${identity.name}`} />
        </span>
      </span>
      {identity.admin_role && <Badge tone="primary">{identity.admin_role === "admin" ? "Admin" : "Support"}</Badge>}
      {busy && <Spinner size={18} />}
    </Card>
  );
}

/**
 * Development sign-in (no Clerk key, NEXT_PUBLIC_AUTH_DEV=true, non-production): pick a seeded
 * identity or type any email. The backend accepts these tokens only with
 * AUTH_DEV_BYPASS=true and APP_ENV=development.
 */
export function DevSignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const dev = useDevAuth();
  const auth = useAuthSession();
  const [busy, setBusy] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState<string | null>(null);

  const signIn = async (identity: { subject: string; email: string; name: string }) => {
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
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-3">
        <Badge tone="warning" size="md" icon={<FlaskConical />} className="self-start">
          Development sign-in
          <InfoTip tone="current" content="Builds without a Clerk key and with NEXT_PUBLIC_AUTH_DEV=true sign in as a seeded or typed account. Production builds always use Clerk." />
        </Badge>
        <h1 className="type-title text-fg">Sign in</h1>
      </div>
      <Stagger as="ul" className="flex flex-col gap-2" aria-label="Seeded accounts">
        {DEV_IDENTITIES.map((identity) => (
          <StaggerItem as="li" key={identity.subject}>
            <IdentityRow identity={identity} busy={busy === identity.subject} onPick={() => void signIn({ subject: identity.subject, email: identity.email, name: identity.name })} />
          </StaggerItem>
        ))}
      </Stagger>
      <form className="flex flex-col gap-4" onSubmit={submitEmail} noValidate>
        <Field label="Any email" info="Signs in as the account with this address, creating it on first use." error={emailError ?? undefined}>
          <Input type="email" inputMode="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="name@scaffoldapp.test" />
        </Field>
        <Button type="submit" variant="secondary" size="auto" className="self-start" loading={busy !== null && busy === subjectFor(email.trim())}>
          Continue
        </Button>
      </form>
    </div>
  );
}
