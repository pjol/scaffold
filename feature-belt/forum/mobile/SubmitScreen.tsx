import { errorMessage, FORUM_LIMITS, forumMentionIds, useCreateForumPost, useForumCommunities, useForumCommunity, useForumPost, useUpdateForumPost, type ForumPost } from "@scaffoldapp/shared";
import { Banner, Button, Chip, EmptyState, ErrorState, Field, Input, Screen, SegmentedControl, Skeleton, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { MessagesSquare } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";
import { ForumMentionInput, type ForumMentionPerson } from "./MentionInput";
import { forumPostHref } from "./parts";

type Kind = "text" | "link";

function Form({ initialCommunity, editing }: { initialCommunity?: string; editing?: ForumPost }) {
  const theme = useTheme();
  const s = theme.space;
  const communities = useForumCommunities();
  const [community, setCommunity] = useState(editing?.community.slug ?? initialCommunity ?? "");
  const detail = useForumCommunity(community || null);
  const create = useCreateForumPost();
  const update = useUpdateForumPost();
  const [kind, setKind] = useState<Kind>(editing?.kind ?? "text");
  const [title, setTitle] = useState(editing?.title ?? "");
  const [body, setBody] = useState(editing?.body ?? "");
  const [url, setUrl] = useState("");
  const [people, setPeople] = useState<ForumMentionPerson[]>(editing?.mentions ?? []);
  const [tried, setTried] = useState(false);
  const busy = create.isPending || update.isPending;

  const locked = !editing && !!detail.data?.locked && !detail.data.can_moderate;
  const titleError = title.trim().length < FORUM_LIMITS.titleMin ? `Give it a title of at least ${FORUM_LIMITS.titleMin} characters.` : null;
  const urlError = !editing && kind === "link" && !/^https?:\/\/\S+$/i.test(url.trim()) ? "Paste a link that starts with http:// or https://." : null;
  const communityError = community ? null : "Choose a community.";

  const submit = async () => {
    setTried(true);
    if (titleError || urlError || communityError || locked) return;
    const mentions = forumMentionIds(`${title}\n${body}`, people);
    try {
      const res = editing
        ? await update.mutateAsync({ id: editing.id, title: title.trim(), body, mentions })
        : await create.mutateAsync({ community, title: title.trim(), body, mentions, ...(kind === "link" ? { url: url.trim() } : {}) });
      if (res.moderation.status === "held") toast.warning("Posted. It's waiting for review, so only you can see it for now.");
      else if (res.moderation.status === "removed") toast.error(`It can't be shown: ${res.moderation.reasons.join(". ") || "it breaks the rules"}.`);
      else toast.success(editing ? "Post saved" : "Posted");
      if (editing) router.back();
      else router.replace(forumPostHref(res.post.id));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't post that. Try again."));
    }
  };

  const list = communities.data?.items ?? [];
  if (communities.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <View style={{ gap: s[4] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton height={48} />
          <Skeleton height={48} />
          <Skeleton height={120} />
        </View>
      </Screen>
    );
  }
  if (communities.isError) {
    return (
      <Screen edges={["bottom"]}>
        <ErrorState title="Couldn't load communities" info={errorMessage(communities.error)} onRetry={() => void communities.refetch()} retrying={communities.isRefetching} />
      </Screen>
    );
  }
  if (list.length === 0) {
    return (
      <Screen edges={["bottom"]}>
        <EmptyState icon={MessagesSquare} title="No communities yet" />
      </Screen>
    );
  }

  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      keyboard
      footer={
        <Button fullWidth loading={busy} disabled={locked} onPress={() => void submit()}>
          {editing ? "Save" : "Post"}
        </Button>
      }
    >
      {editing ? null : (
        <Field label="Community" error={tried ? communityError : null}>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }}>
            {list.map((c) => (
              <Chip key={c.id} size="sm" selected={community === c.slug} onPress={() => setCommunity(c.slug)}>
                {c.title}
              </Chip>
            ))}
          </ScrollView>
        </Field>
      )}
      {locked ? <Banner tone="info" title="This community is locked. Only its moderators can post." /> : null}
      {editing ? null : (
        <SegmentedControl<Kind>
          accessibilityLabel="Post type"
          value={kind}
          onValueChange={setKind}
          options={[
            { value: "text", label: "Text" },
            { value: "link", label: "Link" },
          ]}
        />
      )}
      <Field label="Title" error={tried ? titleError : null}>
        <Input value={title} maxLength={FORUM_LIMITS.titleMax} onChangeText={setTitle} />
      </Field>
      {!editing && kind === "link" ? (
        <Field label="Link" error={tried ? urlError : null}>
          <Input value={url} onChangeText={setUrl} placeholder="https://" keyboardType="url" autoCapitalize="none" autoCorrect={false} />
        </Field>
      ) : null}
      <Field label={kind === "link" ? "Text (optional)" : "Text"} info="Markdown: **bold**, _italic_, [links](https://…), lists and quotes. Type @ to mention someone.">
        <ForumMentionInput value={body} onChange={setBody} people={people} onPeopleChange={setPeople} accessibilityLabel="Text" minHeight={s[32]} />
      </Field>
    </Screen>
  );
}

/** New post (`?community=` preselects one), or edit yours with `?edit=<post id>`. */
export function SubmitScreen({ community, edit }: { community?: string; edit?: string }) {
  const post = useForumPost(edit ?? null);
  if (!edit) return <Form {...(community ? { initialCommunity: community } : {})} />;
  if (post.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Skeleton height={48} />
      </Screen>
    );
  }
  if (post.isError) {
    return (
      <Screen edges={["bottom"]}>
        <ErrorState title="Couldn't load the post" info={errorMessage(post.error)} onRetry={() => void post.refetch()} retrying={post.isRefetching} />
      </Screen>
    );
  }
  return <Form editing={post.data} />;
}

export const forumSubmitHref = (community?: string) => (community ? `/forum/submit?community=${encodeURIComponent(community)}` : "/forum/submit") as Href;
