import { errorMessage, hasErrorCode, socialProfilePath, useMe, useSocialFollow, type SocialActivityItem, type SocialPerson } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Skeleton, Text, toast, useTheme, type ButtonProps } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Activity, CalendarDays, Heart, MessageSquare, Newspaper, Star, Trophy, UserPlus, type LucideIcon } from "lucide-react-native";
import { Pressable, View } from "react-native";

/** The name people read: display name, else @handle. */
export function socialName(p: { display_name: string; handle: string }): string {
  return p.display_name.trim() || `@${p.handle}`;
}

const day = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const dayYear = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

/** "just now", "5 minutes ago", then "Oct 30" (Hermes has no Intl.RelativeTimeFormat). */
export function socialTimeAgo(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"} ago`;
  if (s < 45) return "just now";
  if (s < 3600) return plural(Math.max(1, Math.round(s / 60)), "minute");
  if (s < 86_400) return plural(Math.round(s / 3600), "hour");
  if (s < 7 * 86_400) return plural(Math.round(s / 86_400), "day");
  const d = new Date(t);
  return d.getFullYear() === new Date(now).getFullYear() ? day.format(d) : dayYear.format(d);
}

/** Open an app path from an activity item or profile ("/u/sam"). */
export function openSocialPath(path: string): void {
  if (path.startsWith("/")) router.push(path as Href);
}

export interface FollowButtonProps {
  userId: string;
  following: boolean;
  name: string;
  size?: ButtonProps["size"];
  fullWidth?: boolean;
}

/** Follow / Following. Nothing for the caller's own id. Optimistic through `useSocialFollow`. */
export function FollowButton({ userId, following, name, size = "md", fullWidth }: FollowButtonProps) {
  const me = useMe();
  const follow = useSocialFollow();
  if (!me.data || me.data.id === userId) return null;
  return (
    <Button
      variant={following ? "secondary" : "primary"}
      size={size}
      fullWidth={fullWidth}
      leftIcon={following ? undefined : UserPlus}
      accessibilityState={{ selected: following }}
      accessibilityLabel={following ? `Following ${name}. Unfollow` : `Follow ${name}`}
      disabled={follow.isPending && follow.variables?.userId === userId}
      onPress={() =>
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
        )
      }
    >
      {following ? "Following" : "Follow"}
    </Button>
  );
}

/** One person: photo, name and @handle opening the profile, "Follows you", Follow. */
export function PersonRow({ person, info }: { person: SocialPerson; info?: string }) {
  const theme = useTheme();
  const s = theme.space;
  const name = socialName(person);
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingVertical: s[2], minHeight: theme.tap.lg, borderBottomWidth: 1, borderBottomColor: theme.colors.border }}>
      <Pressable
        accessibilityRole="link"
        accessibilityLabel={`${name}, @${person.handle}${person.follows_you ? ", follows you" : ""}`}
        onPress={() => openSocialPath(socialProfilePath(person.handle))}
        style={{ flex: 1, flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base }}
      >
        <Avatar name={name} src={person.avatar_url} size="md" />
        <View style={{ flex: 1, gap: s[0.5] }}>
          <Text role="body" weight={600} numberOfLines={1}>
            {name}
          </Text>
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2] }}>
            <Text role="label" tone="muted" numberOfLines={1}>
              @{person.handle}
            </Text>
            {person.follows_you ? <Badge size="sm">Follows you</Badge> : null}
          </View>
          {info ? (
            <Text role="caption" tone="muted" numberOfLines={1}>
              {info}
            </Text>
          ) : null}
        </View>
      </Pressable>
      <FollowButton userId={person.user_id} following={person.is_following} name={name} />
    </View>
  );
}

export function PeopleSkeleton({ rows = 5 }: { rows?: number }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View accessibilityLabel="Loading people" accessibilityRole="progressbar" style={{ gap: s[3] }}>
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
          <Skeleton shape="circle" width={40} height={40} />
          <Skeleton width="45%" height={16} />
        </View>
      ))}
    </View>
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

export function activityIcon(name: string): LucideIcon {
  return icons[name] ?? Activity;
}

/** One activity item: who, what, when, and a line of content; opens its link. */
export function ActivityRow({ item, showActor = true }: { item: SocialActivityItem; showActor?: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const Icon = activityIcon(item.icon);
  const actor = socialName(item.actor);
  const when = socialTimeAgo(item.created_at);
  return (
    <Pressable
      accessibilityRole={item.link ? "link" : undefined}
      accessibilityLabel={`${showActor ? `${actor}, ` : ""}${item.title}${item.summary ? `, ${item.summary}` : ""}, ${when}`}
      disabled={!item.link}
      onPress={() => openSocialPath(item.link)}
      style={({ pressed }) => ({
        flexDirection: "row",
        gap: s[3],
        paddingVertical: s[3],
        borderBottomWidth: 1,
        borderBottomColor: theme.colors.border,
        opacity: pressed ? 0.7 : 1,
      })}
    >
      {showActor ? (
        <Avatar name={actor} src={item.actor.avatar_url} size="md" />
      ) : (
        <View style={{ width: 40, height: 40, borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, alignItems: "center", justifyContent: "center" }}>
          <Icon size={20} color={theme.colors.textMuted} />
        </View>
      )}
      <View style={{ flex: 1, gap: s[1] }}>
        <Text role="body">
          {showActor ? <Text role="body" weight={600}>{`${actor} `}</Text> : null}
          {item.title}
        </Text>
        {item.summary ? (
          <Text role="label" tone="muted" numberOfLines={2}>
            {item.summary}
          </Text>
        ) : null}
        <Text role="caption" tone="muted">
          {when}
        </Text>
      </View>
    </Pressable>
  );
}

export function ActivitySkeleton({ rows = 4 }: { rows?: number }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View accessibilityLabel="Loading activity" accessibilityRole="progressbar" style={{ gap: s[4] }}>
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={{ flexDirection: "row", gap: s[3] }}>
          <Skeleton shape="circle" width={40} height={40} />
          <View style={{ flex: 1, gap: s[2] }}>
            <Skeleton width="70%" height={16} />
            <Skeleton width="35%" height={12} />
          </View>
        </View>
      ))}
    </View>
  );
}
