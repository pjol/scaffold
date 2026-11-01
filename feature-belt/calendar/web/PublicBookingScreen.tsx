"use client";

import {
  calendarDeviceZone,
  calendarEndpoints,
  calendarStartOfMonth,
  calendarTodayKey,
  errorMessage,
  formatCalendarDuration,
  formatCalendarRange,
  isApiError,
  orgsNoun,
  useAuthSession,
  useCalendarPublicLink,
  useCalendarPublicSlots,
  useCreateCalendarBooking,
  useEndpoints,
  type CalendarBookingConfirmation,
  type CalendarMembersOnly,
  type CalendarPublicLink,
  type CalendarSlot,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, EmptyState, ErrorState, Field, Input, Select, Skeleton, SkeletonGroup, TextArea, ToggleChip, cn } from "@scaffoldapp/ui-web";
import { CalendarCheck, CalendarPlus, CalendarX, ChevronLeft, Clock, Lock, LogIn, MapPin, Phone, Video } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { signInHref } from "@/lib/redirect";
import { SlotPicker, monthWindow } from "./SlotPicker";

const LOCATION_ICON = { video: <Video />, phone: <Phone />, in_person: <MapPin />, custom: <MapPin /> } as const;

function Fact({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center gap-2 type-label text-fg-muted">
      <span className="flex size-5 shrink-0 items-center justify-center [&_svg]:size-4" aria-hidden>
        {icon}
      </span>
      <span className="min-w-0 break-words">{children}</span>
    </div>
  );
}

/** Add-to-calendar links for a booked time: .ics download, Google and Outlook web. */
export function AddToCalendar({ icsUrl, title, start, end, location }: { icsUrl: string; title: string; start: string; end: string; location: string }) {
  const g = (iso: string) => iso.replace(/[-:]/g, "").replace(/\.\d{3}/, "");
  const google = `https://calendar.google.com/calendar/render?action=TEMPLATE&text=${encodeURIComponent(title)}&dates=${g(start)}/${g(end)}&location=${encodeURIComponent(location)}`;
  const outlook = `https://outlook.live.com/calendar/0/deeplink/compose?path=/calendar/action/compose&rru=addevent&subject=${encodeURIComponent(title)}&startdt=${encodeURIComponent(start)}&enddt=${encodeURIComponent(end)}&location=${encodeURIComponent(location)}`;
  return (
    <div className="flex flex-wrap gap-2">
      <Button asChild variant="secondary" size="sm" leftIcon={<CalendarPlus />}>
        <a href={icsUrl} download>
          Download .ics
        </a>
      </Button>
      <Button asChild variant="ghost" size="sm">
        <a href={google} target="_blank" rel="noopener noreferrer">
          Google Calendar
        </a>
      </Button>
      <Button asChild variant="ghost" size="sm">
        <a href={outlook} target="_blank" rel="noopener noreferrer">
          Outlook
        </a>
      </Button>
    </div>
  );
}

/** The link header: host, title, lengths, location, description. */
export function LinkHeader({ link, duration, onDuration }: { link: Pick<CalendarPublicLink, "host" | "title" | "durations" | "location_type" | "location_label" | "description" | "org_name" | "audience">; duration: number; onDuration?: (d: number) => void }) {
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <AppAvatar name={link.host.display_name} src={link.host.avatar_url} size="md" eager />
        <span className="type-label text-fg-muted">{link.host.display_name}</span>
      </div>
      <h1 className="break-words type-title text-fg lg:type-headline">{link.title}</h1>
      <div className="flex flex-col gap-2">
        {link.durations.length > 1 && onDuration ? (
          <div className="flex flex-wrap gap-2" role="group" aria-label="Meeting length">
            {link.durations.map((d) => (
              <ToggleChip key={d} pressed={d === duration} onPressedChange={() => onDuration(d)} icon={<Clock />}>
                {formatCalendarDuration(d)}
              </ToggleChip>
            ))}
          </div>
        ) : (
          <Fact icon={<Clock />}>{formatCalendarDuration(duration)}</Fact>
        )}
        <Fact icon={LOCATION_ICON[link.location_type]}>{link.location_label}</Fact>
        {link.audience === "internal" && link.org_name ? <Fact icon={<Lock />}>Members of {link.org_name}</Fact> : null}
      </div>
      {link.description ? <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{link.description}</p> : null}
    </div>
  );
}

/** /book/<slug>: pick a time, give details, get a confirmation. Internal links need a signed-in member. */
export function PublicBookingScreen({ slug }: { slug: string }) {
  const auth = useAuthSession();
  const link = useCalendarPublicLink(auth.isLoaded ? slug : null);
  const [zone, setZone] = useState(calendarDeviceZone);
  const [duration, setDuration] = useState<number | null>(null);
  const [month, setMonth] = useState(() => calendarStartOfMonth(calendarTodayKey(calendarDeviceZone())));
  const [slot, setSlot] = useState<CalendarSlot | null>(null);
  const [step, setStep] = useState<"pick" | "details">("pick");
  const [done, setDone] = useState<CalendarBookingConfirmation | null>(null);
  const dur = duration ?? link.data?.durations[0] ?? 0;
  const win = useMemo(() => monthWindow(month, zone, 0), [month, zone]);
  const slots = useCalendarPublicSlots(link.data?.access === "open" ? slug : null, win.from, win.to, dur);

  const frame = (children: ReactNode) => <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">{children}</div>;

  if (!auth.isLoaded || link.isPending) {
    return frame(
      <SkeletonGroup label="Loading booking page" className="flex flex-col gap-4">
        <Skeleton className="size-10 rounded-pill" />
        <Skeleton shape="text" className="h-8 w-1/2" />
        <Skeleton className="h-72" />
      </SkeletonGroup>,
    );
  }
  if (link.isError) {
    const err = link.error;
    if (isApiError(err) && err.code === "members_only") {
      const d = (err.details ?? {}) as Partial<CalendarMembersOnly>;
      return frame(
        <Card className="max-w-form">
          <EmptyState
            icon={<Lock />}
            title={`Only members of ${d.org_name ?? `this ${orgsNoun()}`} can book this link`}
            action={
              !d.signed_in ? (
                <Button asChild size="auto" leftIcon={<LogIn />}>
                  <Link href={signInHref(`/book/${slug}`)}>Sign in</Link>
                </Button>
              ) : undefined
            }
          />
        </Card>,
      );
    }
    if (isApiError(err) && (err.status === 404 || err.status === 410)) {
      return frame(
        <Card className="max-w-form">
          <EmptyState icon={<CalendarX />} title={err.status === 410 ? "This booking link is turned off" : "This booking link doesn't exist"} />
        </Card>,
      );
    }
    return frame(
      <Card className="max-w-form">
        <ErrorState title="Couldn't load this booking page" info={err.message} onRetry={() => void link.refetch()} retrying={link.isFetching} />
      </Card>,
    );
  }

  const l = link.data;
  if (l.access === "members_only") {
    return frame(
      <Card className="max-w-form">
        <EmptyState
          icon={<Lock />}
          title={`Only members of ${l.org_name ?? `this ${orgsNoun()}`} can book this link`}
          action={
            !l.signed_in ? (
              <Button asChild size="auto" leftIcon={<LogIn />}>
                <Link href={signInHref(`/book/${slug}`)}>Sign in</Link>
              </Button>
            ) : undefined
          }
        />
      </Card>,
    );
  }
  if (done) return frame(<Confirmation link={l} done={done} zone={zone} />);

  return frame(
    <div className="grid gap-section lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)]">
      <LinkHeader
        link={l}
        duration={dur}
        onDuration={(d) => {
          setDuration(d);
          setSlot(null);
          setStep("pick");
        }}
      />
      <Card className="min-w-0">
        {step === "pick" ? (
          <SlotPicker
            zone={zone}
            onZoneChange={setZone}
            month={month}
            onMonthChange={setMonth}
            slots={slots.data?.slots ?? []}
            loading={slots.isFetching}
            error={slots.isError ? errorMessage(slots.error) : null}
            onRetry={() => void slots.refetch()}
            selected={slot?.starts_at ?? null}
            onSelect={setSlot}
            onConfirm={() => setStep("details")}
          />
        ) : slot ? (
          <BookingForm
            link={l}
            slug={slug}
            slot={slot}
            zone={zone}
            duration={dur}
            onBack={() => setStep("pick")}
            onTaken={() => {
              setSlot(null);
              setStep("pick");
              void slots.refetch();
            }}
            onDone={setDone}
          />
        ) : null}
      </Card>
    </div>,
  );
}

function BookingForm({
  link,
  slug,
  slot,
  zone,
  duration,
  onBack,
  onTaken,
  onDone,
}: {
  link: CalendarPublicLink;
  slug: string;
  slot: CalendarSlot;
  zone: string;
  duration: number;
  onBack: () => void;
  onTaken: () => void;
  onDone: (c: CalendarBookingConfirmation) => void;
}) {
  const book = useCreateCalendarBooking(slug);
  const [name, setName] = useState(link.booker?.name ?? "");
  const [email, setEmail] = useState(link.booker?.email ?? "");
  const [phone, setPhone] = useState("");
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  const submit = async () => {
    const next: Record<string, string> = {};
    if (!link.booker) {
      if (!name.trim()) next["name"] = "Enter your name.";
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) next["email"] = "Enter a valid email address.";
    }
    if (link.needs_phone && !phone.trim()) next["phone"] = "Enter the number to call.";
    for (const q of link.questions) if (q.required && !answers[q.id]?.trim()) next[q.id] = "Answer this question.";
    setErrors(next);
    if (Object.keys(next).length > 0) return;
    setFormError(null);
    try {
      const res = await book.mutateAsync({
        starts_at: slot.starts_at,
        duration,
        timezone: zone,
        ...(link.booker ? {} : { name: name.trim(), email: email.trim() }),
        ...(phone ? { phone: phone.trim() } : {}),
        answers: Object.entries(answers).map(([question_id, value]) => ({ question_id, value })),
      });
      onDone(res);
    } catch (e) {
      if (isApiError(e) && e.code === "slot_taken") {
        onTaken();
        return;
      }
      setFormError(errorMessage(e, "Couldn't book that time. Try again."));
    }
  };

  return (
    <form
      className="flex max-w-form flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <button type="button" onClick={onBack} className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Change time
      </button>
      <div className="flex items-center gap-2 type-body font-semibold text-fg">
        <CalendarCheck className="size-5 text-primary-strong" aria-hidden />
        {formatCalendarRange({ starts_at: slot.starts_at, ends_at: slot.ends_at, all_day: false, start_date: null, end_date: null }, zone)}
      </div>
      {formError ? <Banner tone="danger">{formError}</Banner> : null}
      {link.booker ? (
        <p className="type-body text-fg">
          Booking as {link.booker.name} ({link.booker.email})
        </p>
      ) : (
        <>
          <Field label="Name" error={errors["name"]} required>
            <Input value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" maxLength={100} />
          </Field>
          <Field label="Email" error={errors["email"]} required info="Your confirmation, the calendar invite and a link to change the booking go here.">
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" maxLength={254} />
          </Field>
        </>
      )}
      {link.needs_phone ? (
        <Field label="Phone number" error={errors["phone"]} required>
          <Input type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} autoComplete="tel" maxLength={40} />
        </Field>
      ) : null}
      {link.questions.map((q) => (
        <Field key={q.id} label={q.label} error={errors[q.id]} required={q.required}>
          {q.type === "long_text" ? (
            <TextArea value={answers[q.id] ?? ""} onChange={(e) => setAnswers({ ...answers, [q.id]: e.target.value })} maxLength={2000} />
          ) : q.type === "choice" ? (
            <Select value={answers[q.id] ?? ""} onValueChange={(v) => setAnswers({ ...answers, [q.id]: v })} placeholder="Choose one" options={q.options.map((o) => ({ value: o, label: o }))} />
          ) : (
            <Input type={q.type === "phone" ? "tel" : "text"} value={answers[q.id] ?? ""} onChange={(e) => setAnswers({ ...answers, [q.id]: e.target.value })} maxLength={2000} />
          )}
        </Field>
      ))}
      <Button type="submit" size="auto" loading={book.isPending} className={cn("self-start")}>
        Confirm booking
      </Button>
    </form>
  );
}

function Confirmation({ link, done, zone }: { link: CalendarPublicLink; done: CalendarBookingConfirmation; zone: string }) {
  const api = useEndpoints(calendarEndpoints);
  const b = done.booking;
  const token = done.manage_url.split("/book/manage/")[1] ?? "";
  return (
    <Card className="mx-auto w-full max-w-form">
      <div className="flex flex-col gap-6">
        <div className="flex flex-col items-start gap-3">
          <span className="flex size-12 items-center justify-center rounded-pill bg-success-soft text-success-strong" aria-hidden>
            <CalendarCheck className="size-6" />
          </span>
          <h1 className="type-title text-fg">You're booked with {b.host.display_name}</h1>
        </div>
        <div className="flex flex-col gap-2">
          <Fact icon={<Clock />}>{formatCalendarRange({ starts_at: b.starts_at, ends_at: b.ends_at, all_day: false, start_date: null, end_date: null }, zone)}</Fact>
          <Fact icon={LOCATION_ICON[b.location_type]}>
            {b.conference_url ? (
              <a href={b.conference_url} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                Join video call
              </a>
            ) : (
              b.location_label
            )}
          </Fact>
        </div>
        {done.confirmation_message ? <p className="whitespace-pre-wrap break-words type-body text-fg">{done.confirmation_message}</p> : null}
        <p className="type-body text-fg-muted">A confirmation with a calendar invite is on its way to {b.booker_email}.</p>
        <AddToCalendar icsUrl={api.manage.icsUrl(token)} title={`${link.title} with ${b.host.display_name}`} start={b.starts_at} end={b.ends_at} location={b.conference_url ?? b.location_label} />
        <Button asChild variant="ghost" size="auto" className="self-start">
          <Link href={`/book/manage/${token}`}>Reschedule or cancel</Link>
        </Button>
      </div>
    </Card>
  );
}
