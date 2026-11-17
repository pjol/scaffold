"use client";

import { errorMessage, hasErrorCode, socialProfilePath, useMe, useSocialFollow, type SocialActivityItem, type SocialPerson } from "@scaffoldapp/shared";
import { Badge, Button, Skeleton, SkeletonGroup, cn, toast, type ButtonProps } from "@scaffoldapp/ui-web";
import { Activity, CalendarDays, Heart, MessageSquare, Newspaper, Star, Trophy, UserPlus, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { formatRelative } from "@/lib/format";

/** The name people read: display name, else @handle. */
export function socialName(p: { display_name: string; handle: string }): string {
  return p.display_name.trim() || `@${p.handle}`;
}

export interface FollowButtonProps {
  userId: string;
  /** Whether the caller follows them now (from the profile or row). */
  following: boolean;
  /** For the toast ("Following Sam"). */
  name: string;
  size?: ButtonProps["size"];
  className?: string;
}

/** Follow / Following. Hidden on the caller's own profile and rows. Optimistic through `useSocialFollow`. */
export function FollowButton({ userId, following, name, size = "sm", className }: FollowButtonProps) {
  const me = useMe();
  const follow = useSocialFollow();
  if (!me.data || me.data.id === userId) return null;
  const toggle = () =>
    follow.mutate(
      { userId, follow: !following },
      {
        onError: (e) =>
          toast.error(
            hasErrorCode(e, "blocked")
              ? "You can't follow this person."
              : errorMessage(e, following ? `Couldn't unfollow ${name}. Try again.` : `Couldn't follow ${name}. Try again.`),
          ),
      },
    );
  return (
    <Button
      variant={following ? "secondary" : "primary"}
      size={size}
      leftIcon={following ? undefined : <UserPlus />}
      aria-pressed={following}
      aria-label={following ? `Following ${name}. Unfollow` : `Follow ${name}`}
      disabled={follow.isPending && follow.variables?.userId === userId}
      onClick={toggle}
      className={className}
    >
      {following ? "Following" : "Follow"}
    </Button>
  );
}

/** One person in a list: photo, name and @handle linking to the profile, "Follows you", Follow. */
export function PersonRow({ person, info }: { person: SocialPerson; info?: string }) {
  const name = socialName(person);
  return (
    <li className="flex min-h-tap-lg items-center gap-3 border-b border-border py-2 last:border-b-0">
      <Link href={socialProfilePath(person.handle)} className="flex min-w-0 flex-1 items-center gap-3 rounded-md focus-visible:outline-2 focus-visible:outline-ring">
        <AppAvatar name={name} src={person.avatar_url} size="md" />
        <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
          <span className="truncate type-body font-medium text-fg">{name}</span>
          <span className="truncate type-label text-fg-muted">@{person.handle}</span>
          {person.follows_you && (
            <Badge size="sm" tone="neutral">
              Follows you
            </Badge>
          )}
          {info && <span className="w-full truncate type-caption text-fg-muted">{info}</span>}
        </span>
      </Link>
      <FollowButton userId={person.user_id} following={person.is_following} name={name} />
    </li>
  );
}

export function PeopleSkeleton({ rows = 5, label = "Loading people" }: { rows?: number; label?: string }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex min-h-tap-lg items-center gap-3 border-b border-border py-2">
          <Skeleton shape="circle" className="size-10" />
          <Skeleton shape="text" className="h-4 w-40" />
          <Skeleton className="ml-auto h-8 w-20" />
        </div>
      ))}
    </SkeletonGroup>
  );
}

const icons: Record<string, LucideIcon> = {
  "user-plus": UserPlus,
  newspaper: Newspaper,
  "message-square": MessageSquare,
  star: Star,
  heart: Heart,
  calendar: CalendarDays,
  trophy: Trophy,
};

/** The kind's icon (registered on the server by name), else a neutral one. */
export function activityIcon(name: string): LucideIcon {
  return icons[name] ?? Activity;
}

/** One activity item: who, what (linking to it), when, and a line of content when there is one. */
export function ActivityRow({ item, showActor = true }: { item: SocialActivityItem; showActor?: boolean }) {
  const Icon = activityIcon(item.icon);
  const actor = socialName(item.actor);
  const body = (
    <>
      <span className="flex flex-wrap items-baseline gap-x-1.5">
        {showActor && <span className="type-body font-medium text-fg">{actor}</span>}
        <span className="type-body text-fg">{item.title}</span>
        <time dateTime={item.created_at} className="type-caption text-fg-muted">
          {formatRelative(item.created_at)}
        </time>
      </span>
      {item.summary && <span className="line-clamp-2 type-label text-fg-muted">{item.summary}</span>}
    </>
  );
  return (
    <li className="flex gap-3 border-b border-border py-3 last:border-b-0">
      {showActor ? (
        <Link href={socialProfilePath(item.actor.handle)} aria-label={actor} className="shrink-0 rounded-full focus-visible:outline-2 focus-visible:outline-ring">
          <AppAvatar name={actor} src={item.actor.avatar_url} size="md" />
        </Link>
      ) : (
        <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-sunken text-fg-muted" aria-hidden>
          <Icon className="size-5" />
        </span>
      )}
      {item.link ? (
        <Link href={item.link} className={cn("flex min-w-0 flex-1 flex-col gap-1 rounded-md focus-visible:outline-2 focus-visible:outline-ring")}>
          {body}
        </Link>
      ) : (
        <div className="flex min-w-0 flex-1 flex-col gap-1">{body}</div>
      )}
    </li>
  );
}

export function ActivitySkeleton({ rows = 4, label = "Loading activity" }: { rows?: number; label?: string }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex gap-3 border-b border-border py-3">
          <Skeleton shape="circle" className="size-10" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton shape="text" className="h-4 w-2/3" />
            <Skeleton shape="text" className="h-4 w-1/3" />
          </div>
        </div>
      ))}
    </SkeletonGroup>
  );
}
