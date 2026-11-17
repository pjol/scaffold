import { docsTitle, errorMessage, useDocsBlockDatabase, useDocsCreateLinkedView, useDocsPage, type DocsPage, type DocsView } from "@scaffoldapp/shared";
import { EmptyState, ErrorState, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { ArrowUpRight, Lock } from "lucide-react-native";
import { useEffect, useRef } from "react";
import { Pressable, View } from "react-native";
import { DatabaseView } from "../DatabaseView";

function SourceTitle({ db, linked }: { db: DocsPage; linked: boolean }) {
  const theme = useTheme();
  const s = theme.space;
  const name = `${db.icon ? `${db.icon} ` : ""}${docsTitle(db.title)}`;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`Open ${docsTitle(db.title)}${linked ? ", the database this view shows" : ""}`}
      onPress={() => router.push(`/docs/${db.id}` as never)}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[1], alignSelf: "flex-start", minHeight: theme.tap.sm, opacity: pressed ? 0.6 : 1 })}
    >
      <Text role="titleSm" numberOfLines={1} style={{ flexShrink: 1 }}>
        {name}
      </Text>
      {linked ? <ArrowUpRight size={18} color={theme.colors.textMuted} /> : null}
    </Pressable>
  );
}

function Loading() {
  const s = useTheme().space;
  return (
    <View style={{ gap: s[2] }}>
      <SkeletonRow />
      <SkeletonRow />
    </View>
  );
}

/** A database block: the database (or a linked view of one) with the block's own views. */
function BlockDatabase({ pageId, blockId }: { pageId: string; blockId: string }) {
  const s = useTheme().space;
  const q = useDocsBlockDatabase(pageId, blockId);
  const create = useDocsCreateLinkedView(pageId, blockId);
  const started = useRef(false);
  const d = q.data;
  useEffect(() => {
    // A linked block nobody has opened yet gets a table view on first open (editors only).
    if (d?.linked && d.database && d.can_edit && d.views.length === 0 && !started.current) {
      started.current = true;
      create.mutate({ database_id: d.database.id, type: "table", name: "Table" });
    }
  }, [d, create]);
  if (q.isPending) return <Loading />;
  if (q.isError) return <ErrorState title="Couldn't load this database." info={errorMessage(q.error)} onRetry={() => void q.refetch()} compact />;
  if (!q.data.database) return <EmptyState icon={Lock} title="You can't open this database." compact />;
  const { database, views, linked, can_edit } = q.data;
  return (
    <View style={{ gap: s[2] }}>
      <SourceTitle db={database} linked={linked} />
      {linked && !views.length && can_edit ? <Loading /> : <DatabaseView db={database} views={views as DocsView[]} editable={can_edit} rowsEditable block={{ pageId, blockId, linked }} />}
    </View>
  );
}

function StandaloneDatabase({ databaseId }: { databaseId: string }) {
  const s = useTheme().space;
  const q = useDocsPage(databaseId);
  if (q.isPending) return <Loading />;
  if (q.isError) {
    return q.error.status === 404 || q.error.status === 403 ? (
      <EmptyState icon={Lock} title="You can't open this database." compact />
    ) : (
      <ErrorState title="Couldn't load this database." info={errorMessage(q.error)} onRetry={() => void q.refetch()} compact />
    );
  }
  const page = q.data.page;
  const canEdit = (page.my_level === "edit" || page.my_level === "manage") && !page.locked && !page.deleted_at;
  return (
    <View style={{ gap: s[2] }}>
      <SourceTitle db={page} linked={false} />
      <DatabaseView db={page} views={q.data.views} editable={canEdit} rowsEditable={!page.deleted_at} />
    </View>
  );
}

export interface InlineDatabaseProps {
  databaseId: string;
  /** The database block and the page it's on: linked views keep their own views per block. */
  blockId?: string | undefined;
  pageId?: string | undefined;
}

/**
 * A database inside a page (Blocks' `renderDatabase(id, blockId)`): with the block and its page, the block's own
 * views (linked databases); otherwise the database's views.
 */
export function InlineDatabase({ databaseId, blockId, pageId }: InlineDatabaseProps) {
  if (pageId && blockId) return <BlockDatabase pageId={pageId} blockId={blockId} />;
  return <StandaloneDatabase databaseId={databaseId} />;
}
