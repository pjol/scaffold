import { docsTitle, errorMessage, orgsNoun, orgsNounA, useActiveOrg, useDocsChildren, useDocsCreatePage, useDocsRecent, useDocsSidebar, type DocsPageSummary } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Field, IconButton, Input, Screen, SegmentedControl, Sheet, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Building2, ChevronDown, ChevronRight, FileText, Plus, Table2 } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";

function TreeRow({ page, depth }: { page: DocsPageSummary; depth: number }) {
  const theme = useTheme();
  const c = theme.colors;
  const [open, setOpen] = useState(false);
  const kids = useDocsChildren(page.id, open);
  return (
    <View>
      <View style={{ flexDirection: "row", alignItems: "center", paddingLeft: depth * theme.space[4] }}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={open ? `Collapse ${docsTitle(page.title)}` : `Expand ${docsTitle(page.title)}`}
          accessibilityState={{ expanded: open }}
          onPress={() => setOpen((o) => !o)}
          disabled={!page.has_children}
          hitSlop={8}
          style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center", opacity: page.has_children ? 1 : 0.3 }}
        >
          {open ? <ChevronDown size={18} color={c.textMuted} /> : <ChevronRight size={18} color={c.textMuted} />}
        </Pressable>
        <Pressable
          accessibilityRole="link"
          onPress={() => router.push(`/docs/${page.id}` as never)}
          style={({ pressed }) => ({ flex: 1, flexDirection: "row", alignItems: "center", gap: theme.space[2], minHeight: theme.tap.base, backgroundColor: pressed ? c.sunken : "transparent", borderRadius: theme.radius.sm })}
        >
          {page.icon ? <Text role="body">{page.icon}</Text> : page.kind === "database" ? <Table2 size={18} color={c.textMuted} /> : <FileText size={18} color={c.textMuted} />}
          <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
            {docsTitle(page.title)}
          </Text>
        </Pressable>
      </View>
      {open ? kids.isPending ? <SkeletonRow /> : (kids.data ?? []).map((k) => <TreeRow key={k.id} page={k} depth={depth + 1} />) : null}
    </View>
  );
}

function Group({ title, pages }: { title: string; pages: DocsPageSummary[] }) {
  const theme = useTheme();
  if (!pages.length) return null;
  return (
    <View style={{ gap: theme.space[1] }}>
      <Text role="label" tone="muted" weight={600}>
        {title}
      </Text>
      {pages.map((p) => (
        <TreeRow key={p.id} page={p} depth={0} />
      ))}
    </View>
  );
}

/** docs: the active org's pages (favorites, shared, private) and recent ones. */
export function DocsHomeScreen() {
  const theme = useTheme();
  const { org, isPending } = useActiveOrg();
  const sidebar = useDocsSidebar(org?.id);
  const recent = useDocsRecent(org?.id);
  const create = useDocsCreatePage(org?.id ?? "");
  const [sheet, setSheet] = useState(false);
  const [title, setTitle] = useState("");
  const [shared, setShared] = useState<"private" | "shared">("private");
  if (isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Docs" }} />
        <SkeletonRow />
        <SkeletonRow />
      </Screen>
    );
  }
  if (!org) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "Docs" }} />
        <EmptyState icon={Building2} title={`Join ${orgsNounA()} to use docs`} />
      </Screen>
    );
  }
  const submit = () =>
    create.mutate(
      { title: title.trim(), ...(shared === "shared" ? { shared: true } : {}) },
      {
        onSuccess: (p) => {
          setSheet(false);
          setTitle("");
          router.push(`/docs/${p.id}` as never);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't create the page.")),
      },
    );
  return (
    <Screen
      edges={["bottom"]}
      scroll
      refreshControl={<RefreshControl refreshing={sidebar.isRefetching} onRefresh={() => void Promise.all([sidebar.refetch(), recent.refetch()])} />}
    >
      <Stack.Screen options={{ title: "Docs", headerRight: () => <IconButton label="New page" icon={Plus} onPress={() => setSheet(true)} /> }} />
      {sidebar.isPending ? (
        <>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </>
      ) : sidebar.isError ? (
        <ErrorState title="Couldn't load the pages." info={errorMessage(sidebar.error)} onRetry={() => void sidebar.refetch()} />
      ) : (
        <View style={{ gap: theme.space[6] }}>
          {recent.data?.length ? <Group title="Recent" pages={recent.data.slice(0, 5)} /> : null}
          <Group title="Favorites" pages={sidebar.data.favorites} />
          <Group title="Shared" pages={sidebar.data.shared} />
          <Group title="Private" pages={sidebar.data.private} />
          {!sidebar.data.shared.length && !sidebar.data.private.length ? (
            <EmptyState
              icon={FileText}
              title="No pages yet"
              action={
                <Button size="md" onPress={() => setSheet(true)}>
                  Write a page
                </Button>
              }
            />
          ) : null}
        </View>
      )}
      <Sheet
        open={sheet}
        onClose={() => setSheet(false)}
        title="New page"
        snapPoints={[360]}
        footer={
          <Button fullWidth loading={create.isPending} onPress={submit}>
            Create
          </Button>
        }
      >
        <View style={{ gap: theme.space[4] }}>
          <Field label="Title">
            <Input value={title} onChangeText={setTitle} placeholder="Untitled" autoFocus maxLength={300} returnKeyType="done" onSubmitEditing={submit} />
          </Field>
          {sidebar.data?.can_create_shared ? (
            <Field label="Who can see it" info={`A private page is only yours until you share it. Everyone means every member of the ${orgsNoun()} can edit it.`}>
              <SegmentedControl<"private" | "shared">
                value={shared}
                onValueChange={setShared}
                options={[
                  { value: "private", label: "Only me" },
                  { value: "shared", label: "Everyone" },
                ]}
              />
            </Field>
          ) : null}
        </View>
      </Sheet>
    </Screen>
  );
}
