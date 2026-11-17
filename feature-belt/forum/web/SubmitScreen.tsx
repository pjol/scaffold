"use client";

import { errorMessage, FORUM_LIMITS, forumMentionIds, useCreateForumPost, useForumCommunities, useForumCommunity } from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, Field, Input, SegmentedControl, Select, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { MessagesSquare } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { ForumMentionTextArea, type ForumMentionPerson } from "./MentionTextArea";
import { forumPostPath } from "./PostList";

type Kind = "text" | "link";

/** /forum/submit?community=: a text or link post to one community. Screening may hold it for review; the post page then says so. */
export function SubmitScreen() {
  const router = useRouter();
  const [community, setCommunity] = useUrlState("community", "");
  const communities = useForumCommunities();
  const detail = useForumCommunity(community || null);
  const create = useCreateForumPost();
  const [kind, setKind] = useState<Kind>("text");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [url, setUrl] = useState("");
  const [people, setPeople] = useState<ForumMentionPerson[]>([]);
  const [tried, setTried] = useState(false);

  const locked = !!detail.data?.locked && !detail.data.can_moderate;
  const titleError = title.trim().length < FORUM_LIMITS.titleMin ? `Give it a title of at least ${FORUM_LIMITS.titleMin} characters.` : undefined;
  const urlError = kind === "link" && !/^https?:\/\/\S+$/i.test(url.trim()) ? "Paste a link that starts with http:// or https://." : undefined;
  const communityError = community ? undefined : "Choose a community.";
  const bodyError = body.length > FORUM_LIMITS.postChars ? `Keep it under ${FORUM_LIMITS.postChars} characters.` : undefined;

  const submit = async () => {
    setTried(true);
    if (titleError || urlError || communityError || bodyError || locked) return;
    try {
      const res = await create.mutateAsync({
        community,
        title: title.trim(),
        body,
        ...(kind === "link" ? { url: url.trim() } : {}),
        mentions: forumMentionIds(`${title}\n${body}`, people),
      });
      if (res.moderation.status === "held") toast.warning("Posted. It's waiting for review, so only you can see it for now.");
      else if (res.moderation.status === "removed") toast.error(`It can't be shown: ${res.moderation.reasons.join(". ") || "it breaks the rules"}.`);
      else toast.success("Posted");
      router.push(forumPostPath(res.post.id));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't post that. Try again."));
    }
  };

  const items = communities.data?.items ?? [];
  return (
    <div className="mx-auto flex w-full max-w-form flex-col gap-section px-gutter py-section">
      <PageHeader title="New post" />
      {communities.isPending ? (
        <SkeletonGroup label="Loading" className="flex flex-col gap-4">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
          <Skeleton className="h-32" />
        </SkeletonGroup>
      ) : communities.isError ? (
        <ErrorState title="Couldn't load communities" info={communities.error.message} onRetry={() => void communities.refetch()} retrying={communities.isFetching} />
      ) : items.length === 0 ? (
        <EmptyState icon={<MessagesSquare />} title="No communities yet" />
      ) : (
        <form
          className="flex flex-col gap-6"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          <Field label="Community" {...(tried && communityError ? { error: communityError } : {})}>
            <Select value={community} onValueChange={setCommunity} placeholder="Choose a community" options={items.map((c) => ({ value: c.slug, label: c.title }))} />
          </Field>
          {locked ? <Banner tone="info">This community is locked. Only its moderators can post.</Banner> : null}
          <SegmentedControl<Kind>
            aria-label="Post type"
            fullWidth={false}
            value={kind}
            onValueChange={setKind}
            options={[
              { value: "text", label: "Text" },
              { value: "link", label: "Link" },
            ]}
          />
          <Field label="Title" {...(tried && titleError ? { error: titleError } : {})}>
            <Input value={title} maxLength={FORUM_LIMITS.titleMax} onChange={(e) => setTitle(e.target.value)} />
          </Field>
          {kind === "link" ? (
            <Field label="Link" {...(tried && urlError ? { error: urlError } : {})}>
              <Input type="url" inputMode="url" value={url} placeholder="https://" onChange={(e) => setUrl(e.target.value)} />
            </Field>
          ) : null}
          <Field
            label={kind === "link" ? "Text (optional)" : "Text"}
            info="Markdown: **bold**, _italic_, [links](https://…), lists and quotes. Type @ to mention someone."
            {...(tried && bodyError ? { error: bodyError } : {})}
          >
            <ForumMentionTextArea value={body} onChange={setBody} people={people} onPeopleChange={setPeople} minRows={6} onSubmit={() => void submit()} />
          </Field>
          <Button type="submit" size="auto" className="self-start" loading={create.isPending} disabled={locked}>
            Post
          </Button>
        </form>
      )}
    </div>
  );
}
