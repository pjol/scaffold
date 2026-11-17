import {
  errorMessage,
  isApiError,
  useIsBlocked,
  useQueryClient,
  useSocialFollowers,
  useSocialFollowing,
  useSocialProfile,
  useSocialProfileActivity,
  type SocialProfile,
  type SocialProfileTab,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, Screen, SegmentedControl, Skeleton, Spinner, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Ban, CalendarDays, Flag, Link2, Lock, Pencil, Rss, UserRoundX, Users } from "lucide-react-native";
import { useState } from "react";
import { Linking, Pressable, RefreshControl, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { BlockButton, BlockDialog, ReportDialog } from "@/features/moderation";
import { ActivityRow, ActivitySkeleton, FollowButton, PeopleSkeleton, PersonRow, socialName } from "./parts";

const joined = new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric" });

function hostOf(url: string): string {
  const m = /^https?:\/\/(?:www\.)?([^/?#]+)/i.exec(url);
  return m?.[1] ?? url;
}

function Header({ p }: { p: SocialProfile }) {
  const theme = useTheme();
  const s = theme.space;
  const qc = useQueryClient();
  const name = socialName(p.user);
  const blocked = useIsBlocked(p.user.user_id);
  const [report, setReport] = useState(false);
  const [block, setBlock] = useState(false);
  const refresh = () => void qc.invalidateQueries({ queryKey: ["social"] });
  const actions: ResourceMenuAction[] = [];
  if (!p.viewer.is_self) {
    actions.push({ id: "report", label: "Report profile", icon: Flag, onSelect: () => setReport(true) });
    if (!blocked) actions.push({ id: "block", label: `Block ${name}`, icon: Ban, destructive: true, onSelect: () => setBlock(true) });
  }
  return (
    <View style={{ gap: s[4] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[4] }}>
        <Avatar name={name} src={p.user.avatar_url} size="xl" />
        <View style={{ flex: 1, gap: s[1] }}>
          <Title numberOfLines={2}>{name}</Title>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
            <Text role="label" tone="muted">
              @{p.user.handle}
            </Text>
            {p.viewer.follows_you ? <Badge size="sm">Follows you</Badge> : null}
          </View>
        </View>
        <ResourceMenu feature="social" title={name} actions={actions} label="Profile actions" />
      </View>
      {p.viewer.is_self ? (
        <Button variant="secondary" leftIcon={Pencil} fullWidth onPress={() => router.push("/settings/public-profile")}>
          Edit profile
        </Button>
      ) : p.restricted ? null : (
        <FollowButton userId={p.user.user_id} following={p.viewer.following} name={name} size="lg" fullWidth />
      )}
      {!p.viewer.is_self ? (
        <>
          <ReportDialog
            open={report}
            onClose={() => setReport(false)}
            targetKind="social.profile"
            targetId={p.user.user_id}
            noun="profile"
            author={{ id: p.user.user_id, name }}
            onReported={({ blocked: b }) => b && refresh()}
          />
          <BlockDialog open={block} onClose={() => setBlock(false)} userId={p.user.user_id} name={name} onBlocked={refresh} />
        </>
      ) : null}
    </View>
  );
}

function About({ p }: { p: SocialProfile }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[3] }}>
      {p.bio ? <Text role="body">{p.bio}</Text> : null}
      {p.links.map((l) => (
        <Pressable
          key={l.url}
          accessibilityRole="link"
          accessibilityLabel={l.label ? `${l.label}, ${hostOf(l.url)}` : hostOf(l.url)}
          onPress={() => void Linking.openURL(l.url)}
          style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.sm }}
        >
          <Link2 size={16} color={theme.colors.primaryStrong} />
          <Text role="label" tone="primary">
            {l.label || hostOf(l.url)}
          </Text>
        </Pressable>
      ))}
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <CalendarDays size={16} color={theme.colors.textMuted} />
        <Text role="label" tone="muted">
          Joined {joined.format(new Date(p.joined_at))}
        </Text>
      </View>
    </View>
  );
}

function More({ hasNextPage, isFetchingNextPage, fetchNextPage }: { hasNextPage: boolean; isFetchingNextPage: boolean; fetchNextPage: () => unknown }) {
  if (isFetchingNextPage) return <Spinner size={20} />;
  if (!hasNextPage) return null;
  return (
    <Button variant="ghost" onPress={() => void fetchNextPage()}>
      Load more
    </Button>
  );
}

function ActivityTab({ handle, self }: { handle: string; self: boolean }) {
  const act = useSocialProfileActivity(handle);
  if (act.isPending) return <ActivitySkeleton />;
  if (act.isError) return <ErrorState compact title="Couldn't load activity" info={errorMessage(act.error)} onRetry={() => void act.refetch()} retrying={act.isFetching} />;
  if (act.data.items.length === 0 && !act.hasNextPage) {
    return (
      <EmptyState
        compact
        icon={Rss}
        title={self ? "Nothing here yet" : "No activity to show"}
        action={
          self ? (
            <Button variant="secondary" onPress={() => router.push("/people")}>
              Find people to follow
            </Button>
          ) : undefined
        }
      />
    );
  }
  return (
    <View>
      {act.data.items.map((it) => (
        <ActivityRow key={it.id} item={it} showActor={false} />
      ))}
      <More hasNextPage={act.hasNextPage} isFetchingNextPage={act.isFetchingNextPage} fetchNextPage={act.fetchNextPage} />
    </View>
  );
}

function PeopleTab({ handle, which }: { handle: string; which: "followers" | "following" }) {
  const followers = useSocialFollowers(handle, which === "followers");
  const following = useSocialFollowing(handle, which === "following");
  const list = which === "followers" ? followers : following;
  if (list.isPending) return <PeopleSkeleton />;
  if (list.isError) return <ErrorState compact title="Couldn't load this list" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isFetching} />;
  if (list.data.items.length === 0) return <EmptyState compact icon={Users} title={which === "followers" ? "No followers yet" : "Not following anyone yet"} />;
  return (
    <View>
      {list.data.items.map((p) => (
        <PersonRow key={p.user_id} person={p} />
      ))}
      <More hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </View>
  );
}

function Restricted({ p }: { p: SocialProfile }) {
  const blocked = useIsBlocked(p.user.user_id);
  const qc = useQueryClient();
  const name = socialName(p.user);
  return blocked ? (
    <EmptyState icon={Ban} title={`You blocked ${name}`} action={<BlockButton userId={p.user.user_id} name={name} onUnblocked={() => void qc.invalidateQueries({ queryKey: ["social"] })} />} />
  ) : (
    <EmptyState icon={UserRoundX} title="This profile isn't available" />
  );
}

/** A profile (`u/<handle>`; "me" and old handles resolve): follow, report, block, and Activity / Followers / Following. */
export function ProfileScreen({ handle, initialTab = "activity" }: { handle: string; initialTab?: SocialProfileTab }) {
  const theme = useTheme();
  const profile = useSocialProfile(handle);
  const [tab, setTab] = useState<SocialProfileTab>(initialTab);
  const p = profile.data;

  const body = (() => {
    if (profile.isPending) {
      return (
        <View accessibilityLabel="Loading profile" accessibilityRole="progressbar" style={{ gap: theme.space[4] }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[4] }}>
            <Skeleton shape="circle" width={64} height={64} />
            <View style={{ flex: 1, gap: theme.space[2] }}>
              <Skeleton width="60%" height={24} />
              <Skeleton width="30%" height={14} />
            </View>
          </View>
          <Skeleton height={theme.tap.base} />
          <ActivitySkeleton rows={3} />
        </View>
      );
    }
    if (profile.isError) {
      return isApiError(profile.error) && profile.error.status === 404 ? (
        <EmptyState
          icon={UserRoundX}
          title="This profile isn't available"
          action={
            <Button variant="secondary" onPress={() => router.push("/people")}>
              Find people
            </Button>
          }
        />
      ) : (
        <ErrorState title="Couldn't load this profile" info={errorMessage(profile.error)} onRetry={() => void profile.refetch()} retrying={profile.isFetching} />
      );
    }
    if (!p) return null;
    if (p.restricted) {
      return (
        <>
          <Header p={p} />
          <Restricted p={p} />
        </>
      );
    }
    return (
      <>
        <Header p={p} />
        <About p={p} />
        <SegmentedControl<SocialProfileTab>
          accessibilityLabel="Profile"
          options={[
            { value: "activity", label: "Activity" },
            { value: "followers", label: `Followers ${p.followers}` },
            { value: "following", label: `Following ${p.following}` },
          ]}
          value={tab}
          onValueChange={setTab}
        />
        {tab === "activity" ? (
          <ActivityTab handle={p.user.handle} self={p.viewer.is_self} />
        ) : p.sections["connections"] !== false ? (
          <PeopleTab key={tab} handle={p.user.handle} which={tab} />
        ) : (
          <EmptyState compact icon={Lock} title={`${socialName(p.user)} keeps this list private`} />
        )}
      </>
    );
  })();

  return (
    <>
      {p ? <Stack.Screen options={{ title: `@${p.user.handle}` }} /> : null}
      <Screen
        edges={["bottom"]}
        gap={theme.space[6]}
        refreshControl={<RefreshControl refreshing={profile.isRefetching} onRefresh={() => void profile.refetch()} tintColor={theme.colors.textMuted} />}
      >
        {body}
      </Screen>
    </>
  );
}
