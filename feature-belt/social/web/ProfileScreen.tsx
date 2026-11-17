"use client";

import {
  isApiError,
  socialProfilePath,
  useIsBlocked,
  useQueryClient,
  useSocialFollowers,
  useSocialFollowing,
  useSocialProfile,
  useSocialProfileActivity,
  type SocialProfile,
  type SocialProfileTab,
} from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Skeleton, SkeletonGroup, Tab, Tabs, TabsList, toast } from "@scaffoldapp/ui-web";
import { Ban, CalendarDays, Flag, Link2, Lock, Pencil, Rss, UserRoundX, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { BlockButton, BlockDialog, ReportDialog } from "@/features/moderation";
import type { ResourceMenuAction } from "@/features/registry";
import { useUrlState } from "@/lib/url-state";
import { ActivityRow, ActivitySkeleton, FollowButton, PeopleSkeleton, PersonRow, socialName } from "./parts";

const joined = new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric" });

function hostOf(url: string): string {
  try {
    return new URL(url).host.replace(/^www\./, "");
  } catch {
    return url;
  }
}

function ProfileSkeleton() {
  return (
    <SkeletonGroup label="Loading profile" className="flex flex-col gap-6">
      <div className="flex items-center gap-4">
        <Skeleton shape="circle" className="size-20" />
        <div className="flex flex-1 flex-col gap-2">
          <Skeleton shape="text" className="h-7 w-48" />
          <Skeleton shape="text" className="h-4 w-24" />
        </div>
      </div>
      <Skeleton shape="text" className="h-4 w-full" />
      <Skeleton shape="text" className="h-4 w-2/3" />
      <ActivitySkeleton rows={3} />
    </SkeletonGroup>
  );
}

function Header({ p }: { p: SocialProfile }) {
  const qc = useQueryClient();
  const name = socialName(p.user);
  const [report, setReport] = useState(false);
  const [block, setBlock] = useState(false);
  const blocked = useIsBlocked(p.user.user_id);
  const refresh = () => void qc.invalidateQueries({ queryKey: ["social"] });
  const copy = () => {
    const url = `${window.location.origin}${socialProfilePath(p.user.handle)}`;
    void navigator.clipboard.writeText(url).then(
      () => toast.success("Link copied"),
      () => toast.error("Couldn't copy the link."),
    );
  };
  const actions: ResourceMenuAction[] = [{ id: "copy", label: "Copy link to profile", icon: Link2, onSelect: copy }];
  if (!p.viewer.is_self) {
    actions.push({ id: "report", label: "Report profile", icon: Flag, onSelect: () => setReport(true) });
    if (!blocked) actions.push({ id: "block", label: `Block ${name}`, icon: Ban, destructive: true, onSelect: () => setBlock(true) });
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-4">
        <AppAvatar name={name} src={p.user.avatar_url} size="xl" eager />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <h1 className="truncate type-title text-fg lg:type-headline">{name}</h1>
          <div className="flex flex-wrap items-center gap-2">
            <span className="type-label text-fg-muted">@{p.user.handle}</span>
            {p.viewer.follows_you && <Badge tone="neutral">Follows you</Badge>}
          </div>
        </div>
        <div className="flex items-center gap-2">
          {p.viewer.is_self ? (
            <Button asChild variant="secondary" size="auto" leftIcon={<Pencil />}>
              <Link href="/settings/public-profile">Edit profile</Link>
            </Button>
          ) : p.restricted ? null : (
            <FollowButton userId={p.user.user_id} following={p.viewer.following} name={name} size="auto" />
          )}
          <ResourceMenu feature="social" title={name} actions={actions} label="Profile actions" />
        </div>
      </div>
      {!p.viewer.is_self && (
        <>
          <ReportDialog
            open={report}
            onOpenChange={setReport}
            targetKind="social.profile"
            targetId={p.user.user_id}
            noun="profile"
            author={{ id: p.user.user_id, name }}
            onReported={({ blocked: b }) => b && refresh()}
          />
          <BlockDialog open={block} onOpenChange={setBlock} userId={p.user.user_id} name={name} onBlocked={refresh} />
        </>
      )}
    </div>
  );
}

function About({ p }: { p: SocialProfile }) {
  return (
    <div className="flex flex-col gap-3">
      {p.bio && <p className="max-w-prose type-body whitespace-pre-line text-fg">{p.bio}</p>}
      {p.links.length > 0 && (
        <ul className="flex flex-wrap gap-x-4 gap-y-2" aria-label="Links">
          {p.links.map((l) => (
            <li key={l.url}>
              <a href={l.url} target="_blank" rel="noopener noreferrer nofollow ugc" className="inline-flex items-center gap-1.5 type-label text-primary-strong underline-offset-4 hover:underline">
                <Link2 className="size-4" aria-hidden />
                {l.label || hostOf(l.url)}
              </a>
            </li>
          ))}
        </ul>
      )}
      <span className="inline-flex items-center gap-1.5 type-label text-fg-muted">
        <CalendarDays className="size-4" aria-hidden />
        Joined {joined.format(new Date(p.joined_at))}
      </span>
    </div>
  );
}

function ActivityTab({ handle, self }: { handle: string; self: boolean }) {
  const act = useSocialProfileActivity(handle);
  if (act.isPending) return <ActivitySkeleton />;
  if (act.isError) return <ErrorState compact title="Couldn't load activity" info={act.error.message} onRetry={() => void act.refetch()} retrying={act.isFetching} />;
  if (act.data.items.length === 0 && !act.hasNextPage) {
    return (
      <EmptyState
        compact
        icon={<Rss />}
        title={self ? "Nothing here yet" : "No activity to show"}
        action={
          self ? (
            <Button asChild variant="secondary" size="auto">
              <Link href="/people">Find people to follow</Link>
            </Button>
          ) : undefined
        }
      />
    );
  }
  return (
    <>
      <ul aria-label="Activity">
        {act.data.items.map((it) => (
          <ActivityRow key={it.id} item={it} showActor={false} />
        ))}
      </ul>
      <LoadMore hasNextPage={act.hasNextPage} isFetchingNextPage={act.isFetchingNextPage} fetchNextPage={act.fetchNextPage} />
    </>
  );
}

function PeopleTab({ handle, which }: { handle: string; which: "followers" | "following" }) {
  const followers = useSocialFollowers(handle, which === "followers");
  const following = useSocialFollowing(handle, which === "following");
  const list = which === "followers" ? followers : following;
  if (list.isPending) return <PeopleSkeleton />;
  if (list.isError) return <ErrorState compact title="Couldn't load this list" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />;
  if (list.data.items.length === 0) {
    return <EmptyState compact icon={<Users />} title={which === "followers" ? "No followers yet" : "Not following anyone yet"} />;
  }
  return (
    <>
      <ul aria-label={which === "followers" ? "Followers" : "Following"}>
        {list.data.items.map((p) => (
          <PersonRow key={p.user_id} person={p} />
        ))}
      </ul>
      <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </>
  );
}

function Restricted({ p }: { p: SocialProfile }) {
  const blocked = useIsBlocked(p.user.user_id);
  const qc = useQueryClient();
  const name = socialName(p.user);
  return blocked ? (
    <EmptyState
      icon={<Ban />}
      title={`You blocked ${name}`}
      action={<BlockButton userId={p.user.user_id} name={name} onUnblocked={() => void qc.invalidateQueries({ queryKey: ["social"] })} />}
    />
  ) : (
    <EmptyState icon={<UserRoundX />} title="This profile isn't available" />
  );
}

/** /u/<handle>: profile, follow, and tabs for activity, followers and following (`?tab=`). Old handles and "me" resolve to the current one. */
export function ProfileScreen({ handle }: { handle: string }) {
  const profile = useSocialProfile(handle);
  const router = useRouter();
  const [tab, setTab] = useUrlState<SocialProfileTab>("tab", "activity");
  const current = profile.data?.user.handle;

  useEffect(() => {
    if (current && current !== decodeURIComponent(handle).toLowerCase()) {
      router.replace(`${socialProfilePath(current)}${window.location.search}`, { scroll: false });
    }
  }, [current, handle, router]);

  const body = (() => {
    if (profile.isPending) return <ProfileSkeleton />;
    if (profile.isError) {
      return isApiError(profile.error) && profile.error.status === 404 ? (
        <EmptyState
          icon={<UserRoundX />}
          title="This profile isn't available"
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href="/people">Find people</Link>
            </Button>
          }
        />
      ) : (
        <ErrorState title="Couldn't load this profile" info={profile.error.message} onRetry={() => void profile.refetch()} retrying={profile.isFetching} />
      );
    }
    const p = profile.data;
    if (p.restricted) {
      return (
        <>
          <Header p={p} />
          <Restricted p={p} />
        </>
      );
    }
    const listsVisible = p.sections["connections"] !== false;
    return (
      <>
        <Header p={p} />
        <About p={p} />
        <Tabs value={tab} onValueChange={(v) => setTab(v as SocialProfileTab)}>
          <TabsList aria-label="Profile">
            <Tab value="activity">Activity</Tab>
            <Tab value="followers" count={p.followers}>
              Followers
            </Tab>
            <Tab value="following" count={p.following}>
              Following
            </Tab>
          </TabsList>
        </Tabs>
        <section aria-label={tab} className="flex flex-col">
          {tab === "activity" ? (
            <ActivityTab handle={p.user.handle} self={p.viewer.is_self} />
          ) : listsVisible ? (
            <PeopleTab key={tab} handle={p.user.handle} which={tab} />
          ) : (
            <EmptyState compact icon={<Lock />} title={`${socialName(p.user)} keeps this list private`} />
          )}
        </section>
      </>
    );
  })();

  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <div className="mx-auto flex w-full max-w-prose flex-col gap-6">{body}</div>
    </div>
  );
}
