import { errorMessage, useForumCommunity } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, EmptyState, ErrorState, InfoTip, Label, ListGroup, Screen, Skeleton, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, type Href } from "expo-router";
import { FileQuestion, Lock, MessagesSquare, SquarePen } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { ForumPostFeed } from "./ForumHomeScreen";
import { ForumPostRow } from "./parts";

/** /forum/c/[slug]: one community's pinned posts and feed, with its rules and moderators behind "About". */
export function CommunityScreen({ slug }: { slug: string }) {
  const theme = useTheme();
  const s = theme.space;
  const community = useForumCommunity(slug);
  const [about, setAbout] = useState(false);

  if (community.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Community" }} />
        <View style={{ gap: s[4] }} accessibilityLabel="Loading community" accessibilityRole="progressbar">
          <Skeleton width="60%" height={28} />
          <Skeleton height={120} />
        </View>
      </Screen>
    );
  }
  if (community.isError) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Community" }} />
        {community.error.status === 404 ? (
          <EmptyState icon={FileQuestion} title="Community not found" />
        ) : (
          <ErrorState title="Couldn't load this community" info={errorMessage(community.error)} onRetry={() => void community.refetch()} retrying={community.isRefetching} />
        )}
      </Screen>
    );
  }

  const c = community.data;
  const canPost = !c.locked || c.can_moderate;
  const compose = () => router.push(`/forum/submit?community=${encodeURIComponent(c.slug)}` as Href);
  const header = (
    <View style={{ gap: s[4] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Title style={{ flexShrink: 1 }}>{c.title}</Title>
        {c.description ? <InfoTip content={c.description} /> : null}
        {c.locked ? (
          <Badge size="sm" tone="neutral" icon={Lock}>
            Locked
          </Badge>
        ) : null}
      </View>
      <View style={{ flexDirection: "row", gap: s[2] }}>
        {canPost ? (
          <Button size="md" leftIcon={SquarePen} onPress={compose}>
            New post
          </Button>
        ) : null}
        <Button size="md" variant="secondary" onPress={() => setAbout((x) => !x)}>
          {about ? "Hide rules" : "Rules and moderators"}
        </Button>
      </View>
      {about ? (
        <View style={{ gap: s[4] }}>
          <ListGroup title="Rules">
            {c.rules.map((r, i) => (
              <View key={i} style={{ flexDirection: "row", gap: s[2], paddingVertical: s[2] }}>
                <Label tone="muted">{`${i + 1}.`}</Label>
                <Label style={{ flex: 1 }}>{r}</Label>
              </View>
            ))}
          </ListGroup>
          {c.moderators.length > 0 ? (
            <ListGroup title="Moderators" info="They pin, lock and remove posts here. Staff review reports and held posts.">
              {c.moderators.map((m) => (
                <View key={m.id} style={{ flexDirection: "row", alignItems: "center", gap: s[2], paddingVertical: s[2] }}>
                  <Avatar name={m.display_name || "?"} src={m.avatar_url} size="sm" />
                  <Text role="label">{m.display_name || "Unnamed"}</Text>
                </View>
              ))}
            </ListGroup>
          ) : null}
        </View>
      ) : null}
      {c.pinned.length > 0 ? (
        <View>
          {c.pinned.map((p) => (
            <ForumPostRow key={p.id} post={p} showCommunity={false} />
          ))}
        </View>
      ) : null}
    </View>
  );

  return (
    <>
      <Stack.Screen options={{ title: c.title }} />
      <ForumPostFeed
        community={c.slug}
        showCommunity={false}
        header={header}
        empty={
          <EmptyState
            icon={MessagesSquare}
            title="No posts here yet"
            {...(canPost
              ? {
                  action: (
                    <Button variant="secondary" onPress={compose}>
                      Write the first post
                    </Button>
                  ),
                }
              : {})}
          />
        }
      />
    </>
  );
}
