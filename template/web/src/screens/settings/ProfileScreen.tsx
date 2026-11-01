"use client";

import { errorMessage, MAX_UPLOAD_BYTES, useMe, useUpdateMe, useUpload } from "@scaffoldapp/shared";
import { Button, Field, Input, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Camera } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Screen, Section } from "@/components/shell/PageHeader";

const PHOTO_TYPES = ["image/jpeg", "image/png", "image/webp"];

function PhotoSection({ name, src }: { name: string; src: string | null }) {
  const upload = useUpload();
  const update = useUpdateMe();
  const input = useRef<HTMLInputElement>(null);
  const busy = upload.isPending || update.isPending;

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    if (!PHOTO_TYPES.includes(file.type)) {
      toast.error("Choose a JPEG, PNG or WebP image.");
      return;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.error("That photo is over 10 MB. Choose a smaller one.");
      return;
    }
    try {
      const r = await upload.mutateAsync({ file, purpose: "avatar" });
      if (!r.public_url) throw new Error("Upload failed. Try again.");
      await update.mutateAsync({ avatar_url: r.public_url });
      toast.success("Photo updated");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't update your photo. Try again."));
    }
  };

  const remove = () => {
    update.mutate(
      { avatar_url: "" },
      { onSuccess: () => toast.success("Photo removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove your photo. Try again.")) },
    );
  };

  return (
    <Section title="Photo">
      <div className="flex flex-wrap items-center gap-4">
        <AppAvatar name={name} src={src} size="xl" eager />
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" size="auto" leftIcon={<Camera />} loading={upload.isPending} disabled={busy} onClick={() => input.current?.click()}>
            {src ? "Change photo" : "Upload photo"}
          </Button>
          {src && (
            <Button variant="ghost" size="auto" disabled={busy} onClick={remove}>
              Remove
            </Button>
          )}
        </div>
        <input
          ref={input}
          type="file"
          accept={PHOTO_TYPES.join(",")}
          hidden
          onChange={(e) => {
            void onFile(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
    </Section>
  );
}

/** Settings → Profile: photo (presigned `avatar` upload) and name. */
export function ProfileScreen() {
  const me = useMe();
  const update = useUpdateMe();
  const saved = me.data?.display_name ?? "";
  const [name, setName] = useState(saved);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setName(saved), [saved]);

  const dirty = name.trim() !== saved;
  const save = (e: FormEvent) => {
    e.preventDefault();
    const next = name.trim();
    if (!next) {
      setError("Enter your name.");
      return;
    }
    setError(null);
    update.mutate({ display_name: next }, { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) });
  };

  if (!me.data) {
    return (
      <SkeletonGroup label="Loading profile" className="flex flex-col gap-section">
        <Skeleton className="h-8 w-40" />
        <div className="flex items-center gap-4">
          <Skeleton shape="circle" className="size-20" />
          <Skeleton className="h-10 w-36" />
        </div>
        <div className="flex max-w-form flex-col gap-4">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
      </SkeletonGroup>
    );
  }

  return (
    <Screen title="Profile">
      <PhotoSection name={me.data.display_name || me.data.email} src={me.data.avatar_url} />
      <form className="flex max-w-form flex-col gap-6" onSubmit={save} noValidate>
        <div className="flex flex-col gap-4">
          <Field label="Name" error={error ?? undefined} required>
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (error) setError(null);
              }}
              maxLength={60}
              autoComplete="name"
            />
          </Field>
          <Field label="Email" info="The address you sign in with. It changes when you change it at sign-in.">
            <Input value={me.data.email} readOnly className="text-fg-muted" />
          </Field>
        </div>
        <Button type="submit" size="auto" className="self-start" loading={update.isPending && update.variables?.display_name !== undefined} disabled={!dirty}>
          Save
        </Button>
      </form>
    </Screen>
  );
}
