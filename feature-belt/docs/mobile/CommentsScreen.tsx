import {
  DOCS_QUICK_REACTIONS,
  docsTimeAgo,
  errorMessage,
  useAccessDirectory,
  useDocsComments,
  useDocsCreateComment,
  useDocsDeleteComment,
  useDocsEditComment,
  useDocsPage,
  useDocsReact,
  useDocsResolveComment,
  useMe,
  type DocsComment,
  type DocsRefs,
  type DocsSpan,
} from "@scaffoldapp/shared";
import { Avatar, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, ListRow, Screen, SegmentedControl, Sheet, SkeletonRow, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, useLocalSearchParams } from "expo-router";
import { Flag, MessageSquare, MoreHorizontal, Pencil, SmilePlus, Trash2 } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Pressable, View } from "react-native";
import { ReportDialog } from "@/features/moderation";
import { RichText } from "./RichText";

type Person = { id: string; name: string };

/** Comment text → spans, with the people picked from "@" as mentions. */
function toSpans(text: string, people: Person[]): DocsSpan[] {
  const out: DocsSpan[] = [];
  let rest = text;
  while (rest) {
    let best: { at: number; p: Person } | null = null;
    for (const p of people) {
      const at = rest.indexOf(`@${p.name}`);
      if (at >= 0 && (!best || at < best.at)) best = { at, p };
    }
    if (!best) {
      out.push({ text: rest });
      break;
    }
    if (best.at > 0) out.push({ text: rest.slice(0, best.at) });
    out.push({ text: `@${best.p.name}`, mention: { type: "user", id: best.p.id } });
    rest = rest.slice(best.at + best.p.name.length + 1);
  }
  return out;
}

function fromSpans(spans: DocsSpan[]): { text: string; people: Person[] } {
  const people: Person[] = [];
  let text = "";
  for (const s of spans) {
    if (s.mention?.type === "user") people.push({ id: s.mention.id, name: s.text.replace(/^@/, "") });
    text += s.text;
  }
  return { text, people };
}

/** A text box with "@" to mention people in the workspace. */
function Composer({ orgId, placeholder, initial, submitLabel, busy, onSubmit, onCancel }: { orgId: string; placeholder: string; initial?: DocsSpan[]; submitLabel: string; busy?: boolean; onSubmit: (body: DocsSpan[]) => Promise<unknown>; onCancel?: () => void }) {
  const theme = useTheme();
  const start = useMemo(() => fromSpans(initial ?? []), [initial]);
  const [text, setText] = useState(start.text);
  const [people, setPeople] = useState<Person[]>(start.people);
  const query = /(^|\s)@([^\s@]{0,30})$/.exec(text)?.[2] ?? null;
  const dir = useAccessDirectory(orgId, query ?? "", { types: "user", limit: 5, enabled: query !== null });
  const pick = (p: Person) => {
    if (query === null) return;
    setText(`${text.slice(0, text.length - query.length - 1)}@${p.name} `);
    setPeople((l) => (l.some((x) => x.id === p.id) ? l : [...l, p]));
  };
  return (
    <View style={{ gap: theme.space[2] }}>
      <TextArea value={text} onChangeText={setText} placeholder={placeholder} accessibilityLabel={placeholder} maxLength={4000} />
      {query !== null && (dir.data?.length ?? 0) > 0 ? (
        <View accessibilityLabel="Mention someone" style={{ borderWidth: 1, borderColor: theme.colors.border, borderRadius: theme.radius.md }}>
          {dir.data!.map((p) => (
            <ListRow key={p.id} title={p.name} leading={<Avatar size="xs" name={p.name} src={p.avatar_url} />} chevron={false} onPress={() => pick({ id: p.id, name: p.name })} />
          ))}
        </View>
      ) : null}
      <View style={{ flexDirection: "row", gap: theme.space[2] }}>
        {onCancel ? (
          <Button size="md" variant="ghost" style={{ flex: 1 }} onPress={onCancel}>
            Cancel
          </Button>
        ) : null}
        <Button
          size="md"
          style={{ flex: 1 }}
          disabled={!text.trim()}
          loading={busy}
          onPress={() =>
            void onSubmit(toSpans(text.trim(), people)).then(() => {
              setText("");
              setPeople([]);
            })
          }
        >
          {submitLabel}
        </Button>
      </View>
    </View>
  );
}

function Reactions({ c, pageId, canReact, onAdd }: { c: DocsComment; pageId: string; canReact: boolean; onAdd: () => void }) {
  const theme = useTheme();
  const react = useDocsReact(pageId);
  const list = c.reactions ?? [];
  if (!list.length && !canReact) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: theme.space[1] }}>
      {list.map((r) => (
        <Pressable
          key={r.emoji}
          accessibilityRole="button"
          accessibilityState={{ selected: r.mine }}
          accessibilityLabel={`${r.emoji} ${r.count}${r.mine ? ", you reacted" : ""}`}
          disabled={!canReact}
          onPress={() => react.mutate({ commentId: c.id, emoji: r.emoji, on: !r.mine }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the reaction.")) })}
          style={{
            flexDirection: "row",
            alignItems: "center",
            gap: theme.space[1],
            minHeight: 32,
            paddingHorizontal: theme.space[2],
            borderRadius: theme.radius.pill,
            borderWidth: 1,
            borderColor: r.mine ? theme.colors.primary : theme.colors.border,
            backgroundColor: r.mine ? theme.colors.primarySoft : "transparent",
          }}
        >
          <Text role="label">{r.emoji}</Text>
          <Text role="caption" tone="muted">
            {r.count}
          </Text>
        </Pressable>
      ))}
      {canReact ? <IconButton label="Add a reaction" icon={SmilePlus} onPress={onAdd} /> : null}
    </View>
  );
}

function Item({ c, pageId, orgId, refs, canComment, canManage, onReact }: { c: DocsComment; pageId: string; orgId: string; refs: DocsRefs; canComment: boolean; canManage: boolean; onReact: (c: DocsComment) => void }) {
  const theme = useTheme();
  const me = useMe();
  const edit = useDocsEditComment(pageId);
  const del = useDocsDeleteComment(pageId);
  const [menu, setMenu] = useState(false);
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [report, setReport] = useState(false);
  if (c.deleted) {
    return (
      <Text role="label" tone="subtle" style={{ fontStyle: "italic" }}>
        Comment deleted
      </Text>
    );
  }
  const mine = !!c.author && c.author.id === me.data?.id;
  return (
    <View style={{ flexDirection: "row", gap: theme.space[2] }}>
      <Avatar name={c.author?.name ?? "Deleted user"} src={c.author?.avatar_url} size="sm" />
      <View style={{ flex: 1, gap: theme.space[1] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
            {c.author?.name ?? "Deleted user"}
          </Text>
          <Text role="caption" tone="muted">
            {docsTimeAgo(c.created_at)}
            {c.edited_at ? " · edited" : ""}
          </Text>
          <View style={{ flex: 1 }} />
          <IconButton label="Comment actions" icon={MoreHorizontal} onPress={() => setMenu(true)} />
        </View>
        {editing ? (
          <Composer
            orgId={orgId}
            placeholder="Edit comment"
            initial={c.body}
            submitLabel="Save"
            busy={edit.isPending}
            onCancel={() => setEditing(false)}
            onSubmit={(body) =>
              edit.mutateAsync({ id: c.id, body }).then(
                () => setEditing(false),
                (e) => toast.error(errorMessage(e, "Couldn't save the comment.")),
              )
            }
          />
        ) : (
          <>
            <RichText spans={c.body} refs={refs} role="body" />
            <Reactions c={c} pageId={pageId} canReact={canComment} onAdd={() => onReact(c)} />
          </>
        )}
      </View>
      <Sheet open={menu} onClose={() => setMenu(false)} title="Comment" snapPoints={[260]}>
        <View>
          {mine ? <ListRow title="Edit" leading={<Pencil size={18} color={theme.colors.text} />} chevron={false} onPress={() => (setMenu(false), setEditing(true))} /> : null}
          {canComment ? <ListRow title="Add a reaction" leading={<SmilePlus size={18} color={theme.colors.text} />} chevron={false} onPress={() => (setMenu(false), onReact(c))} /> : null}
          {!mine ? <ListRow title="Report" leading={<Flag size={18} color={theme.colors.text} />} chevron={false} onPress={() => (setMenu(false), setReport(true))} /> : null}
          {mine || canManage ? <ListRow title="Delete" destructive leading={<Trash2 size={18} color={theme.colors.dangerStrong} />} chevron={false} onPress={() => (setMenu(false), setConfirm(true))} /> : null}
        </View>
      </Sheet>
      <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} title="Delete this comment?" confirmLabel="Delete" tone="danger" errorText={(e) => errorMessage(e, "Couldn't delete the comment.")} onConfirm={() => del.mutateAsync(c.id)}>
        <Text role="body">The thread keeps its place; the text goes.</Text>
      </ConfirmDialog>
      <ReportDialog open={report} onClose={() => setReport(false)} targetKind="docs.comment" targetId={c.id} noun="comment" author={c.author ? { id: c.author.id, name: c.author.name } : null} />
    </View>
  );
}

function Thread({ t, pageId, orgId, refs, canComment, canManage, onReact }: { t: DocsComment; pageId: string; orgId: string; refs: DocsRefs; canComment: boolean; canManage: boolean; onReact: (c: DocsComment) => void }) {
  const theme = useTheme();
  const create = useDocsCreateComment(pageId);
  const resolve = useDocsResolveComment(pageId);
  return (
    <View style={{ gap: theme.space[3], padding: theme.space[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: theme.colors.border, backgroundColor: theme.colors.surface }}>
      {t.quote ? (
        <Text role="label" numberOfLines={2} style={{ backgroundColor: theme.colors.warningSoft, paddingHorizontal: theme.space[2], paddingVertical: theme.space[1], borderRadius: theme.radius.sm }}>
          {t.quote}
        </Text>
      ) : null}
      <Item c={t} pageId={pageId} orgId={orgId} refs={refs} canComment={canComment} canManage={canManage} onReact={onReact} />
      {t.replies.map((r) => (
        <Item key={r.id} c={r} pageId={pageId} orgId={orgId} refs={refs} canComment={canComment} canManage={canManage} onReact={onReact} />
      ))}
      {canComment && !t.resolved_at ? (
        <>
          <Composer
            orgId={orgId}
            placeholder="Reply"
            submitLabel="Reply"
            busy={create.isPending}
            onSubmit={(body) => create.mutateAsync({ thread_id: t.id, body }).catch((e: unknown) => toast.error(errorMessage(e, "Couldn't send the reply.")))}
          />
          <Button size="md" variant="secondary" loading={resolve.isPending} onPress={() => resolve.mutate({ id: t.id, resolved: true }, { onError: (e) => toast.error(errorMessage(e, "Couldn't resolve the thread.")) })}>
            Resolve
          </Button>
        </>
      ) : null}
      {canComment && t.resolved_at ? (
        <Button size="md" variant="ghost" onPress={() => resolve.mutate({ id: t.id, resolved: false })}>
          Reopen
        </Button>
      ) : null}
    </View>
  );
}

const MORE_REACTIONS = ["🙏", "🔥", "💯", "🤔", "😂", "😮", "😢", "🚀", "👏", "⭐", "💡", "❌"];

/** docs/[id]/comments: threads on a page; reply with mentions, react, edit, delete, report, resolve, reopen. */
export function CommentsScreen() {
  const theme = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const page = useDocsPage(id);
  const [status, setStatus] = useState<"open" | "resolved" | "all">("open");
  const threads = useDocsComments(id, status);
  const create = useDocsCreateComment(id);
  const react = useDocsReact(id);
  const [reacting, setReacting] = useState<DocsComment | null>(null);
  const level = page.data?.page.my_level;
  const canComment = (level === "comment" || level === "edit" || level === "manage") && !page.data?.page.deleted_at;
  const canManage = level === "manage";
  const refs = page.data?.refs ?? { users: {}, pages: {} };
  const orgId = page.data?.page.org_id ?? "";
  const pickReaction = (emoji: string) => {
    const c = reacting;
    setReacting(null);
    if (c) react.mutate({ commentId: c.id, emoji, on: !(c.reactions ?? []).some((r) => r.emoji === emoji && r.mine) }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the reaction.")) });
  };
  return (
    <Screen
      edges={["bottom"]}
      scroll
      keyboard
      footer={
        canComment && orgId ? (
          <Composer
            orgId={orgId}
            placeholder="Add a comment"
            submitLabel="Comment"
            busy={create.isPending}
            onSubmit={(body) => create.mutateAsync({ body }).catch((e: unknown) => toast.error(errorMessage(e, "Couldn't post the comment.")))}
          />
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: "Comments" }} />
      <SegmentedControl<"open" | "resolved" | "all">
        value={status}
        onValueChange={setStatus}
        options={[
          { value: "open", label: "Open" },
          { value: "resolved", label: "Resolved" },
          { value: "all", label: "All" },
        ]}
      />
      {threads.isPending ? (
        <>
          <SkeletonRow />
          <SkeletonRow />
        </>
      ) : threads.isError ? (
        <ErrorState title="Couldn't load comments." info={errorMessage(threads.error)} onRetry={() => void threads.refetch()} />
      ) : threads.data.length === 0 ? (
        <EmptyState icon={MessageSquare} title={status === "open" ? "No open comments" : status === "resolved" ? "No resolved comments" : "No comments yet"} />
      ) : (
        threads.data.map((t) => <Thread key={t.id} t={t} pageId={id} orgId={orgId} refs={refs} canComment={canComment} canManage={canManage} onReact={setReacting} />)
      )}
      <Sheet open={!!reacting} onClose={() => setReacting(null)} title="React" snapPoints={[240]}>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[1] }}>
          {[...DOCS_QUICK_REACTIONS, ...MORE_REACTIONS].map((e) => (
            <Pressable key={e} accessibilityRole="button" accessibilityLabel={`React ${e}`} onPress={() => pickReaction(e)} style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center" }}>
              <Text style={{ fontSize: 26 }}>{e}</Text>
            </Pressable>
          ))}
        </View>
      </Sheet>
    </Screen>
  );
}
