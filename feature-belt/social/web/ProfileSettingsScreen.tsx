"use client";

import {
  errorMessage,
  normalizeSocialHandle,
  SOCIAL_HANDLE_PATTERN,
  useChangeSocialHandle,
  useSocialHandleCheck,
  useSocialMe,
  useUpdateSocialProfile,
  type SocialLink,
  type SocialMyProfile,
  type SocialVisibility,
} from "@scaffoldapp/shared";
import { Banner, Button, ErrorState, Field, IconButton, Input, Select, Skeleton, SkeletonGroup, Spinner, TextArea, toast } from "@scaffoldapp/ui-web";
import { AtSign, Check, ExternalLink, Plus, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState, type FormEvent } from "react";
import { Screen, Section } from "@/components/shell/PageHeader";

const VISIBILITY: { value: SocialVisibility; label: string }[] = [
  { value: "public", label: "Everyone" },
  { value: "followers", label: "Followers" },
  { value: "private", label: "Only me" },
];

const day = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

function days(hours: number): string {
  const d = Math.round(hours / 24);
  return d === 1 ? "1 day" : `${d} days`;
}

function HandleSection({ me }: { me: SocialMyProfile }) {
  const [value, setValue] = useState(me.user.handle);
  useEffect(() => setValue(me.user.handle), [me.user.handle]);
  const h = normalizeSocialHandle(value);
  const changed = h !== me.user.handle;
  const shape = SOCIAL_HANDLE_PATTERN.test(h);
  const check = useSocialHandleCheck(value, me.user.handle);
  const change = useChangeSocialHandle();
  const locked = me.handle_change_at ? new Date(me.handle_change_at) > new Date() : false;
  const settled = check.handle === h;

  let error: string | undefined;
  if (changed && h.length > 0 && !shape) error = "Use 3–20 letters, numbers or underscores, starting with a letter.";
  else if (changed && settled && check.data && !check.data.available) error = check.data.message;

  const ready = changed && shape && settled && check.data?.available === true && !locked;
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (!ready) return;
    change.mutate(h, {
      onSuccess: (next) => toast.success(`You're @${next.user.handle} now`),
      onError: (err) => toast.error(errorMessage(err, "Couldn't change your handle. Try again.")),
    });
  };
  const info =
    `Old links to @${me.user.handle} keep working for ${days(me.limits.handle_redirect_hours)}.` +
    (me.limits.handle_cooldown_hours > 0 ? ` You can change your handle once every ${days(me.limits.handle_cooldown_hours)}.` : "");

  return (
    <Section title="Handle">
      <form className="flex max-w-form flex-col gap-6" onSubmit={save} noValidate>
        {locked && me.handle_change_at && <Banner tone="neutral">You can change your handle again on {day.format(new Date(me.handle_change_at))}.</Banner>}
        <Field label="Handle" info={info} error={error}>
          <Input
            value={value}
            onChange={(e) => setValue(e.target.value)}
            leadingIcon={<AtSign />}
            maxLength={21}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            disabled={locked}
            trailing={
              changed && shape && !error ? (
                check.isFetching || !settled ? (
                  <span className="flex size-10 items-center justify-center">
                    <Spinner size={16} label="Checking" />
                  </span>
                ) : check.data?.available ? (
                  <span className="flex size-10 items-center justify-center text-success-strong" aria-label="Available">
                    <Check className="size-5" />
                  </span>
                ) : null
              ) : null
            }
          />
        </Field>
        <Button type="submit" size="auto" className="self-start" loading={change.isPending} disabled={!ready}>
          Change handle
        </Button>
      </form>
    </Section>
  );
}

function AboutSection({ me }: { me: SocialMyProfile }) {
  const [bio, setBio] = useState(me.bio);
  const [links, setLinks] = useState<SocialLink[]>(me.links);
  useEffect(() => {
    setBio(me.bio);
    setLinks(me.links);
  }, [me.bio, me.links]);
  const update = useUpdateSocialProfile();
  const max = me.limits.bio_max_chars;
  const clean = links.map((l) => ({ url: l.url.trim(), label: l.label.trim() })).filter((l) => l.url);
  const dirty = bio.trim() !== me.bio || JSON.stringify(clean) !== JSON.stringify(me.links);
  const over = [...bio].length > max;
  const setLink = (i: number, patch: Partial<SocialLink>) => setLinks((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  const save = (e: FormEvent) => {
    e.preventDefault();
    if (over) return;
    update.mutate(
      { bio: bio.trim(), links: clean },
      { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) },
    );
  };

  return (
    <Section title="About">
      <form className="flex max-w-form flex-col gap-6" onSubmit={save} noValidate>
        <Field
          label="Bio"
          error={over ? `Keep it to ${max} characters.` : undefined}
          labelEnd={
            <span className={over ? "type-caption text-danger-strong" : "type-caption text-fg-muted"}>
              {[...bio].length}/{max}
            </span>
          }
        >
          <TextArea value={bio} onChange={(e) => setBio(e.target.value)} minRows={3} maxRows={8} />
        </Field>
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-2 type-label text-fg">Links</legend>
          {links.map((l, i) => (
            <div key={i} className="flex flex-wrap items-center gap-2 sm:flex-nowrap">
              <Input aria-label={`Link ${i + 1} label`} placeholder="Label" value={l.label} maxLength={40} onChange={(e) => setLink(i, { label: e.target.value })} wrapperClassName="w-full sm:w-40" />
              <Input aria-label={`Link ${i + 1} address`} placeholder="https://" type="url" inputMode="url" value={l.url} onChange={(e) => setLink(i, { url: e.target.value })} wrapperClassName="min-w-0 flex-1" />
              <IconButton label={`Remove link ${i + 1}`} icon={<X />} variant="ghost" size="sm" onClick={() => setLinks((ls) => ls.filter((_, j) => j !== i))} />
            </div>
          ))}
          {links.length < me.limits.max_links && (
            <Button variant="ghost" size="sm" className="self-start" leftIcon={<Plus />} onClick={() => setLinks((ls) => [...ls, { url: "", label: "" }])}>
              Add link
            </Button>
          )}
        </fieldset>
        <Button type="submit" size="auto" className="self-start" loading={update.isPending && update.variables?.bio !== undefined} disabled={!dirty || over}>
          Save
        </Button>
      </form>
    </Section>
  );
}

function PrivacySection({ me }: { me: SocialMyProfile }) {
  const update = useUpdateSocialProfile();
  const set = (key: string, v: SocialVisibility) =>
    update.mutate(
      { privacy: { [key]: v } },
      { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) },
    );
  return (
    <Section title="Who sees what" info="Your name, photo, handle, bio, links and follower counts are visible to everyone signed in.">
      <div className="flex max-w-form flex-col gap-4">
        {me.privacy.map((row) => (
          <Field key={row.key} label={row.label}>
            <Select options={VISIBILITY} value={row.visibility} onValueChange={(v) => v !== row.visibility && set(row.key, v as SocialVisibility)} />
          </Field>
        ))}
      </div>
    </Section>
  );
}

/** Settings → Public profile: handle, bio, links, and who sees each part of the profile. */
export function ProfileSettingsScreen() {
  const me = useSocialMe();
  if (me.isPending) {
    return (
      <SkeletonGroup label="Loading your profile" className="flex flex-col gap-section">
        <Skeleton className="h-8 w-48" />
        <div className="flex max-w-form flex-col gap-4">
          <Skeleton className="h-16" />
          <Skeleton className="h-28" />
          <Skeleton className="h-16" />
        </div>
      </SkeletonGroup>
    );
  }
  if (me.isError) {
    return <ErrorState title="Couldn't load your profile" info={me.error.message} onRetry={() => void me.refetch()} retrying={me.isFetching} />;
  }
  return (
    <Screen
      title="Public profile"
      info="Display name and photo are in Settings → Profile."
      action={
        <Button asChild variant="ghost" size="sm" rightIcon={<ExternalLink />}>
          <Link href={`/u/${me.data.user.handle}`}>View profile</Link>
        </Button>
      }
    >
      <HandleSection me={me.data} />
      <AboutSection me={me.data} />
      <PrivacySection me={me.data} />
    </Screen>
  );
}
