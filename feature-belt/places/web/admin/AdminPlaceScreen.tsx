"use client";

import { errorMessage, formatDay, useAdminDeletePlace, useAdminPlace, useAdminRemovePlacePhoto, useApprovePlace, useMe, useRejectPlace, WEEKDAYS, type AdminPlace } from "@scaffoldapp/shared";
import { Banner, Button, Card, ConfirmDialog, Dialog, ErrorState, Field, IconButton, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { Check, ChevronLeft, ExternalLink, Pencil, Trash2, X } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { isOptimisable } from "@/lib/images";
import { PlaceStatusBadge, useCategoryLabel } from "../parts";

function RejectDialog({ place }: { place: AdminPlace }) {
  const reject = useRejectPlace();
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    const r = reason.trim();
    if (r.length < 3) {
      setError("Give a reason of at least 3 characters. The submitter sees it.");
      return;
    }
    reject.mutate(
      { id: place.id, reason: r },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success("Place rejected");
        },
        onError: (e) => setError(errorMessage(e, "Couldn't reject it. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={`Reject ${place.name}?`}
      trigger={
        <Button variant="danger-ghost" size="auto" leftIcon={<X />}>
          Reject
        </Button>
      }
      footer={
        <>
          <Button variant="ghost" size="auto" onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant="danger" size="auto" loading={reject.isPending} onClick={submit}>
            Reject place
          </Button>
        </>
      }
    >
      <Field label="Reason" required error={error ?? undefined} info="The submitter sees this.">
        <TextArea value={reason} maxLength={500} minRows={3} autoGrow onChange={(e) => setReason(e.target.value)} placeholder="Add the street number to the address." />
      </Field>
    </Dialog>
  );
}

function Details({ place }: { place: AdminPlace }) {
  const label = useCategoryLabel();
  const rows: [string, string][] = [
    ["Category", label(place.category)],
    ["Address", place.address || "—"],
    ["Coordinates", `${place.lat.toFixed(6)}, ${place.lng.toFixed(6)}`],
    ["Phone", place.phone || "—"],
    ["Website", place.website || "—"],
    ["Submitted by", place.owner ? `${place.owner.display_name || place.owner.email} (${place.owner.email})` : "Staff"],
    ["Added", formatDateTime(place.created_at)],
    ["Updated", formatDateTime(place.updated_at)],
  ];
  if (place.reviewed_at) rows.push(["Reviewed", formatDateTime(place.reviewed_at)]);
  return (
    <dl className="grid gap-x-6 gap-y-2 type-label sm:grid-cols-[10rem_minmax(0,1fr)]">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-fg-muted">{k}</dt>
          <dd className="break-words text-fg">{v}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Admin → Places → one place: review (approve, reject with a reason), edit, photos, delete (admin role). */
export function AdminPlaceScreen({ id }: { id: string }) {
  const router = useRouter();
  const me = useMe();
  const place = useAdminPlace(id);
  const approve = useApprovePlace();
  const removePhoto = useAdminRemovePlacePhoto();
  const del = useAdminDeletePlace();

  return (
    <div className="flex flex-col gap-2">
      <Link href="/admin/places" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Places
      </Link>
      {place.isPending ? (
        <SkeletonGroup label="Loading place" className="flex flex-col gap-section">
          <Skeleton className="h-8 w-56" />
          <Skeleton className="h-40" />
        </SkeletonGroup>
      ) : place.isError ? (
        <ErrorState title="Couldn't load this place" info={errorMessage(place.error)} onRetry={() => void place.refetch()} retrying={place.isFetching} />
      ) : (
        <div className="flex flex-col gap-section">
          <div className="flex flex-col gap-3">
            <PageHeader
              title={place.data.name}
              action={
                <Button asChild variant="secondary" size="sm" leftIcon={<Pencil />}>
                  <Link href={`/admin/places/${id}/edit`}>Edit</Link>
                </Button>
              }
            />
            <div className="flex flex-wrap items-center gap-2">
              <PlaceStatusBadge status={place.data.status} />
              {place.data.status === "approved" ? (
                <Button asChild variant="ghost" size="sm" rightIcon={<ExternalLink />}>
                  <Link href={`/places/${id}`}>View public page</Link>
                </Button>
              ) : null}
            </div>
            {place.data.status === "rejected" && place.data.review_note ? <Banner tone="danger">{place.data.review_note}</Banner> : null}
          </div>

          <div className="flex flex-wrap gap-2">
              {place.data.status !== "approved" ? (
                <Button
                  size="auto"
                  leftIcon={<Check />}
                  loading={approve.isPending}
                  onClick={() =>
                    approve.mutate(id, {
                      onSuccess: () => toast.success("Place approved"),
                      onError: (e) => toast.error(errorMessage(e, "Couldn't approve it. Try again.")),
                    })
                  }
                >
                  Approve
                </Button>
              ) : null}
              {place.data.status !== "rejected" ? <RejectDialog place={place.data} /> : null}
          </div>

          <Section title="Details">
            <Card>
              <Details place={place.data} />
            </Card>
          </Section>

          {place.data.description ? (
            <Section title="Description">
              <p className="max-w-prose whitespace-pre-line type-body text-fg">{place.data.description}</p>
            </Section>
          ) : null}

          {place.data.hours ? (
            <Section title="Hours">
              <dl className="grid grid-cols-[8rem_minmax(0,1fr)] gap-y-1 type-label">
                {WEEKDAYS.map((d) => (
                  <div key={d.key} className="contents">
                    <dt className="text-fg-muted">{d.label}</dt>
                    <dd className="tabular-nums text-fg">{formatDay(place.data.hours?.weekly[d.key] ?? [])}</dd>
                  </div>
                ))}
              </dl>
            </Section>
          ) : null}

          <Section title="Photos">
            {place.data.photos.length === 0 ? (
              <p className="type-label text-fg-muted">No photos.</p>
            ) : (
              <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                {place.data.photos.map((ph) => (
                  <li key={ph.id} className="relative aspect-square overflow-hidden rounded-md bg-sunken">
                    <Image src={ph.url} alt="" fill sizes="(min-width: 640px) 25vw, 50vw" className="object-cover" unoptimized={!isOptimisable(ph.url)} />
                    <IconButton
                      label="Remove photo"
                      icon={<Trash2 />}
                      size="sm"
                      variant="inverse"
                      className="absolute top-2 right-2"
                      onClick={() =>
                        removePhoto.mutate(
                          { id, photoId: ph.id },
                          { onSuccess: () => toast.success("Photo removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove it. Try again.")) },
                        )
                      }
                    />
                  </li>
                ))}
              </ul>
            )}
          </Section>

          {me.data?.admin_role === "admin" ? (
            <div>
              <ConfirmDialog
                trigger={
                  <Button variant="danger-ghost" size="auto" leftIcon={<Trash2 />}>
                    Delete place
                  </Button>
                }
                title={`Delete ${place.data.name}?`}
                confirmLabel="Delete place"
                tone="danger"
                onConfirm={async () => {
                  await del.mutateAsync(id);
                  toast.success("Place deleted");
                  router.replace("/admin/places");
                }}
                errorText={(e) => errorMessage(e, "Couldn't delete it. Try again.")}
              >
                Its photos go too. This can't be undone.
              </ConfirmDialog>
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}
