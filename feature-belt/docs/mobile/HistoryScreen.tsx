import {
  docsTimeAgo,
  errorMessage,
  useDocsHistory,
  useDocsPage,
  useDocsRestoreSnapshot,
  useDocsUpdates,
  useDocsVersionDiff,
  type DocsDiffEntry,
  type DocsSnapshot,
} from "@scaffoldapp/shared";
import { AvatarGroup, Button, ConfirmDialog, EmptyState, ErrorState, ListRow, Screen, SegmentedControl, Sheet, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { History, MessageSquare, RotateCcw } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";

const LABEL: Record<DocsDiffEntry["change"], string> = { "": "", added: "Added", removed: "Removed", changed: "Edited" };

function entryText(e: DocsDiffEntry): string {
  const p = e.block.props;
  if (Array.isArray(p.text)) return p.text.map((s) => s.text).join("");
  if (typeof p.code === "string") return p.code;
  if (typeof p.name === "string") return p.name;
  return e.block.type.replace(/_/g, " ");
}

/** What a version changed (or what changed since), block by block. */
function Diff({ snapshotId, against }: { snapshotId: string; against: "previous" | "current" }) {
  const theme = useTheme();
  const c = theme.colors;
  const diff = useDocsVersionDiff(snapshotId, against);
  if (diff.isPending) return <SkeletonRow />;
  if (diff.isError) return <ErrorState title="Couldn't compare the versions." onRetry={() => void diff.refetch()} />;
  const s = diff.data.summary;
  const changed = diff.data.entries.filter((e) => e.change !== "");
  const border = { added: c.success, removed: c.danger, changed: c.warning, "": c.border };
  const bg = { added: c.successSoft, removed: c.dangerSoft, changed: c.warningSoft, "": "transparent" };
  return (
    <View style={{ gap: theme.space[2] }}>
      <Text role="label" tone="muted">
        {s.title_changed ? `Renamed from “${diff.data.from_title}” to “${diff.data.to_title}”. ` : ""}
        {changed.length === 0 ? (against === "current" ? "Nothing has changed since this version." : "No changes to the text in this version.") : `${s.added} added, ${s.changed} edited, ${s.removed} removed`}
      </Text>
      {changed.map((e) => (
        <View
          key={`${e.block.id}-${e.change}`}
          accessibilityLabel={`${LABEL[e.change]}: ${entryText(e)}`}
          style={{ borderLeftWidth: 4, borderLeftColor: border[e.change], backgroundColor: bg[e.change], borderRadius: theme.radius.sm, paddingHorizontal: theme.space[2], paddingVertical: theme.space[1], marginLeft: Math.min(e.depth, 4) * theme.space[3] }}
        >
          <Text role="caption" tone="muted" weight={600}>
            {LABEL[e.change]}
          </Text>
          <Text role="body" style={e.change === "removed" ? { textDecorationLine: "line-through" } : undefined}>
            {entryText(e) || " "}
          </Text>
          {e.change === "changed" && e.before_text ? (
            <Text role="caption" tone="muted" numberOfLines={3}>
              Before: {e.before_text}
            </Text>
          ) : null}
        </View>
      ))}
    </View>
  );
}

/** docs/[id]/history: saved versions (what changed, restore) and the updates feed. */
export function HistoryScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const theme = useTheme();
  const page = useDocsPage(id);
  const history = useDocsHistory(id);
  const updates = useDocsUpdates(id);
  const restore = useDocsRestoreSnapshot(id);
  const [tab, setTab] = useState<"versions" | "updates">("versions");
  const [open, setOpen] = useState<DocsSnapshot | null>(null);
  const [against, setAgainst] = useState<"previous" | "current">("previous");
  const [confirm, setConfirm] = useState(false);
  const level = page.data?.page.my_level;
  const canRestore = (level === "edit" || level === "manage") && !page.data?.page.locked && !page.data?.page.deleted_at;
  const pick = (snapshotId: string) => {
    const s = history.data?.items.find((x) => x.id === snapshotId);
    if (s) setOpen(s);
  };
  return (
    <Screen edges={["bottom"]} scroll>
      <Stack.Screen options={{ title: "Page history" }} />
      <SegmentedControl<"versions" | "updates">
        value={tab}
        onValueChange={setTab}
        options={[
          { value: "versions", label: "Versions" },
          { value: "updates", label: "Updates" },
        ]}
      />
      {tab === "versions" ? (
        history.isPending ? (
          <SkeletonRow />
        ) : history.isError ? (
          <ErrorState title="Couldn't load the history." info={errorMessage(history.error)} onRetry={() => void history.refetch()} />
        ) : history.data.items.length === 0 ? (
          <EmptyState icon={History} title="No saved versions yet" />
        ) : (
          <View>
            {history.data.items.map((s) => (
              <ListRow
                key={s.id}
                title={new Date(s.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
                trailing={
                  <Text role="caption" tone="muted">
                    {s.reason === "before_restore" ? "Before a restore" : docsTimeAgo(s.created_at)}
                  </Text>
                }
                leading={<AvatarGroup people={s.editors.map((e) => ({ name: e.name, src: e.avatar_url }))} size="xs" />}
                onPress={() => setOpen(s)}
              />
            ))}
            {history.hasNextPage ? (
              <Button variant="ghost" size="md" loading={history.isFetchingNextPage} onPress={() => void history.fetchNextPage()}>
                Show more
              </Button>
            ) : null}
          </View>
        )
      ) : updates.isPending ? (
        <SkeletonRow />
      ) : updates.isError ? (
        <ErrorState title="Couldn't load the updates." info={errorMessage(updates.error)} onRetry={() => void updates.refetch()} />
      ) : updates.data.items.length === 0 ? (
        <EmptyState icon={History} title="No updates yet" />
      ) : (
        <View>
          {updates.data.items.map((u) => (
            <Pressable
              key={`${u.type}-${u.snapshot_id ?? u.thread_id}-${u.at}`}
              accessibilityRole="button"
              onPress={() => (u.snapshot_id ? pick(u.snapshot_id) : router.push(`/docs/${id}/comments` as never))}
              style={({ pressed }) => ({ flexDirection: "row", gap: theme.space[3], paddingVertical: theme.space[3], backgroundColor: pressed ? theme.colors.sunken : "transparent", borderBottomWidth: 1, borderBottomColor: theme.colors.border })}
            >
              {u.type === "comment" ? <MessageSquare size={18} color={theme.colors.textMuted} /> : <History size={18} color={theme.colors.textMuted} />}
              <View style={{ flex: 1, gap: theme.space[0.5] }}>
                <Text role="label" weight={600}>
                  {`${u.people.map((p) => p.name).join(", ") || "Someone"}${u.type === "comment" ? " commented" : ""}`}
                </Text>
                <Text role="caption" tone="muted" numberOfLines={2}>
                  {u.text}
                </Text>
              </View>
              <Text role="caption" tone="muted">
                {docsTimeAgo(u.at)}
              </Text>
            </Pressable>
          ))}
          {updates.hasNextPage ? (
            <Button variant="ghost" size="md" loading={updates.isFetchingNextPage} onPress={() => void updates.fetchNextPage()}>
              Show more
            </Button>
          ) : null}
        </View>
      )}
      <Sheet
        open={!!open}
        onClose={() => setOpen(null)}
        title={open ? new Date(open.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : ""}
        snapPoints={["85%"]}
        {...(canRestore && open
          ? {
              footer: (
                <Button fullWidth leftIcon={RotateCcw} onPress={() => setConfirm(true)}>
                  Restore this version
                </Button>
              ),
            }
          : {})}
      >
        {open ? (
          <View style={{ gap: theme.space[3] }}>
            <SegmentedControl<"previous" | "current">
              value={against}
              onValueChange={setAgainst}
              options={[
                { value: "previous", label: "What changed" },
                { value: "current", label: "Since then" },
              ]}
            />
            <Diff snapshotId={open.id} against={against} />
          </View>
        ) : null}
      </Sheet>
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Restore this version?"
        confirmLabel="Restore"
        errorText={(e) => errorMessage(e, "Couldn't restore that version.")}
        onConfirm={() =>
          restore.mutateAsync(open!.id).then(() => {
            setOpen(null);
            toast.success("Version restored.");
            router.back();
          })
        }
      >
        <Text role="body">The page goes back to how it was. Its current content is saved in the history first.</Text>
      </ConfirmDialog>
    </Screen>
  );
}
