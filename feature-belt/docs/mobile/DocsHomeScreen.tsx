import {
  docsTitle,
  errorMessage,
  orgsNoun,
  orgsNounA,
  useActiveOrg,
  useDocsChildren,
  useDocsCreatePage,
  useDocsImportRun,
  useDocsRecent,
  useDocsSidebar,
  useDocsSources,
  useDocsStartImport,
  useDocsTemplates,
  useDocsUseTemplate,
  type DocsPageSummary,
} from "@scaffoldapp/shared";
import {
  Button,
  ConnectPrompt,
  ConnectedSection,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  ListRow,
  Screen,
  SegmentedControl,
  Sheet,
  SkeletonRow,
  Text,
  connectedSectionItems,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Building2, ChevronDown, ChevronRight, FileText, FileUp, Plus, Users } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";
import { docsPickImportFile } from "./pageActions";
import { PageIcon } from "./PageIcon";

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
          <PageIcon icon={page.icon} custom={page.custom_icon} kind={page.kind} size="xs" />
          <Text role="body" numberOfLines={1} style={{ flex: 1 }}>
            {docsTitle(page.title)}
          </Text>
        </Pressable>
      </View>
      {open ? kids.isPending ? <SkeletonRow /> : (kids.data ?? []).map((k) => <TreeRow key={k.id} page={k} depth={depth + 1} />) : null}
    </View>
  );
}

/** Connected Notion workspaces and Google accounts (or a quiet offer to connect one). */
function Connected({ orgId }: { orgId: string }) {
  const sources = useDocsSources(orgId);
  const [dismissed, setDismissed] = useState(false);
  const connected = (sources.data?.sources ?? []).filter((x) => x.type === "connected");
  const offers = sources.data?.offers ?? [];
  if (sources.isError) return null;
  if (!sources.isPending && connected.length === 0) {
    if (dismissed || offers.length === 0) return null;
    return (
      <ConnectPrompt
        offers={offers}
        title="Work with Notion and Google Docs here"
        info="Read and edit Notion pages and Google Docs with your own account. Notion shows only the pages you pick when connecting."
        onConnect={(o) => router.push(o.connect_url as never)}
        onDismiss={() => setDismissed(true)}
      />
    );
  }
  return (
    <ConnectedSection
      items={connectedSectionItems(connected)}
      loading={sources.isPending}
      onSelect={(id) => router.push(`/docs/c/${encodeURIComponent(id)}` as never)}
      onFix={(item) => item.fixUrl && router.push(item.fixUrl as never)}
      {...(offers[0] ? { onAdd: () => router.push(offers[0]!.connect_url as never) } : {})}
    />
  );
}

function Group({ title, pages, icon }: { title: string; pages: DocsPageSummary[]; icon?: boolean }) {
  const theme = useTheme();
  if (!pages.length) return null;
  return (
    <View style={{ gap: theme.space[1] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[1] }}>
        {icon ? <Users size={14} color={theme.colors.textMuted} /> : null}
        <Text role="label" tone="muted" weight={600}>
          {title}
        </Text>
      </View>
      {pages.map((p) => (
        <TreeRow key={p.id} page={p} depth={0} />
      ))}
    </View>
  );
}

/** An import's progress; opens the page when it's done. */
function ImportStatus({ runId, orgId, onOpen }: { runId: string; orgId: string; onOpen: (id: string) => void }) {
  const theme = useTheme();
  const run = useDocsImportRun(runId, orgId);
  if (run.isPending) return <SkeletonRow />;
  if (run.isError) return <ErrorState title="Couldn't check the import." onRetry={() => void run.refetch()} />;
  const r = run.data;
  const pct = r.pages_total ? r.pages_done / r.pages_total : r.status === "done" ? 1 : 0.05;
  return (
    <View style={{ gap: theme.space[2] }} accessibilityLiveRegion="polite">
      <Text role="body">{r.name}</Text>
      <View style={{ height: 8, borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, overflow: "hidden" }} accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 100, now: Math.round(pct * 100) }}>
        <View style={{ width: `${Math.round(pct * 100)}%`, height: "100%", backgroundColor: r.status === "failed" ? theme.colors.danger : theme.colors.primary }} />
      </View>
      <Text role="caption" tone="muted">
        {r.status === "running" ? `${r.pages_done} of ${r.pages_total || "…"} pages` : r.status === "done" ? `${r.pages_done} pages, ${r.files_done} files` : (r.message ?? "Stopped")}
      </Text>
      {r.errors.length ? (
        <Text role="caption" tone="muted">
          {r.errors.length} {r.errors.length === 1 ? "thing" : "things"} couldn&apos;t be imported: {r.errors.slice(0, 3).map((e) => `${e.path} (${e.message})`).join("; ")}
        </Text>
      ) : null}
      {r.root_page_id && r.status !== "running" ? (
        <Button size="md" onPress={() => onOpen(r.root_page_id!)}>
          Open
        </Button>
      ) : null}
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
  const [mode, setMode] = useState<"empty" | "template" | "import">("empty");
  const [runId, setRunId] = useState<string | null>(null);
  const templates = useDocsTemplates(org?.id, sheet && mode === "template");
  const useTpl = useDocsUseTemplate(org?.id ?? "");
  const starter = useDocsStartImport(org?.id ?? "");
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
          {(sidebar.data.teamspaces ?? []).map((t) => (
            <Group key={t.team.id} title={t.team.name} pages={t.pages} icon />
          ))}
          <Group title="Shared" pages={sidebar.data.shared} />
          <Group title="Private" pages={sidebar.data.private} />
          <Connected orgId={org.id} />
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
        onClose={() => {
          setSheet(false);
          setRunId(null);
        }}
        title="New page"
        snapPoints={mode === "empty" ? [400] : ["80%"]}
        {...(mode === "empty"
          ? {
              footer: (
                <Button fullWidth loading={create.isPending} onPress={submit}>
                  Create
                </Button>
              ),
            }
          : {})}
      >
        <View style={{ gap: theme.space[4] }}>
          <SegmentedControl<"empty" | "template" | "import">
            value={mode}
            onValueChange={setMode}
            options={[
              { value: "empty", label: "Empty" },
              { value: "template", label: "Template" },
              { value: "import", label: "Import" },
            ]}
          />
          {sidebar.data?.can_create_shared && mode !== "import" ? (
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
          {mode === "empty" ? (
            <Field label="Title">
              <Input value={title} onChangeText={setTitle} placeholder="Untitled" autoFocus maxLength={300} returnKeyType="done" onSubmitEditing={submit} />
            </Field>
          ) : mode === "template" ? (
            templates.isPending ? (
              <SkeletonRow />
            ) : templates.isError ? (
              <ErrorState title="Couldn't load templates." onRetry={() => void templates.refetch()} />
            ) : (
              <View>
                {[...templates.data.workspace, ...templates.data.builtin].map((t) => (
                  <ListRow
                    key={t.id}
                    title={t.name}
                    {...(t.info ? { info: t.info } : {})}
                    leading={<PageIcon icon={t.icon} custom={t.custom_icon} kind={t.kind} size="sm" />}
                    disabled={useTpl.isPending}
                    onPress={() =>
                      useTpl.mutate(
                        { templateId: t.id, ...(shared === "shared" ? { shared: true } : {}) },
                        {
                          onSuccess: (p) => {
                            setSheet(false);
                            router.push(`/docs/${p.id}` as never);
                          },
                          onError: (e) => toast.error(errorMessage(e, "Couldn't make a page from that template.")),
                        },
                      )
                    }
                  />
                ))}
              </View>
            )
          ) : runId ? (
            <ImportStatus
              runId={runId}
              orgId={org.id}
              onOpen={(id) => {
                setSheet(false);
                setRunId(null);
                router.push(`/docs/${id}` as never);
              }}
            />
          ) : (
            <View style={{ gap: theme.space[2] }}>
              <Text role="body" tone="muted">
                Markdown, text, HTML, Word (.docx), CSV, or a .zip of them: a Notion, Confluence or Markdown export.
              </Text>
              <Button
                size="md"
                leftIcon={FileUp}
                loading={starter.isPending}
                onPress={async () => {
                  const f = await docsPickImportFile();
                  if (!f) return;
                  starter.mutate(
                    { file: { uri: f.uri, name: f.name, mimeType: f.mimeType }, name: f.name },
                    { onSuccess: (r) => setRunId(r.id), onError: (e) => toast.error(errorMessage(e, "Couldn't import that file.")) },
                  );
                }}
              >
                Choose a file
              </Button>
            </View>
          )}
        </View>
      </Sheet>
    </Screen>
  );
}
