import {
  CRM_ACTIVITY_LABEL,
  CRM_KIND,
  CRM_LIFECYCLE_LABEL,
  CRM_TYPE_LABEL,
  crmBodySegments,
  crmDuration,
  crmMoney,
  crmOutcomeLabel,
  crmUpcoming,
  errorMessage,
  topic,
  useCrmMoveStage,
  useCrmPipelines,
  useCrmRecord,
  useCrmTimeline,
  useTopic,
  type CrmActivity,
  type CrmAssociations,
  type CrmDeal,
  type CrmEvent,
  type CrmObjectType,
  type CrmRecord,
  type CrmRecordRef,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorState,
  Headline,
  ListGroup,
  ListRow,
  Screen,
  SegmentedControl,
  Sheet,
  Skeleton,
  Text,
  toast,
  useTheme,
} from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { ArrowRightLeft, Check, History, Mail, NotebookPen, Phone, SearchX } from "lucide-react-native";
import { useState } from "react";
import { Linking, RefreshControl, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { LogSheet } from "./LogSheet";
import { TaskRow } from "./TasksScreen";
import { CrmGate, FactRow, StageBadge, TYPE_ICON, dayLabel, recordPath } from "./parts";

type Pane = "activity" | "about" | "related";

/** A record on mobile: call/email/log actions, timeline, fields and related records. */
export function CrmRecordScreen({ type, id }: { type: CrmObjectType; id: string }) {
  return <CrmGate>{({ orgId }) => <RecordView type={type} id={id} orgId={orgId} />}</CrmGate>;
}

function suggestions(record: CrmRecord, a: CrmAssociations): CrmRecordRef[] {
  if (record.type === "contact") return a.companies.filter((c) => c.is_primary).map((c) => ({ type: "company" as const, id: c.id, name: c.name }));
  if (record.type === "deal" && record.company) return [record.company];
  return [];
}

function RecordView({ type, id, orgId }: { type: CrmObjectType; id: string; orgId: string }) {
  const theme = useTheme();
  const s = theme.space;
  const rec = useCrmRecord(type, id);
  const timeline = useCrmTimeline(type, id);
  useTopic(topic("crm", id));
  const [pane, setPane] = useState<Pane>("activity");
  const [logging, setLogging] = useState(false);
  const [moving, setMoving] = useState(false);

  if (rec.isPending) {
    return (
      <Screen edges={["bottom"]} gap={s[4]}>
        <Stack.Screen options={{ title: CRM_TYPE_LABEL[type].One }} />
        <Skeleton shape="text" width="70%" height={theme.type.headline.lineHeight} />
        <Skeleton height={theme.tap.lg} />
        <Skeleton height={theme.tap.lg * 3} />
      </Screen>
    );
  }
  if (rec.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: CRM_TYPE_LABEL[type].One }} />
        {rec.error.status === 404 ? (
          <EmptyState icon={SearchX} title={`${CRM_TYPE_LABEL[type].One} not found`} info="It was deleted, merged, or isn't shared with you." />
        ) : (
          <ErrorState title={`Couldn't load this ${CRM_TYPE_LABEL[type].one}`} info={rec.error.message} onRetry={() => void rec.refetch()} retrying={rec.isRefetching} />
        )}
      </Screen>
    );
  }
  const { record, associations } = rec.data;
  const canComment = record.my_level !== "none" && record.my_level !== "view";
  const canEdit = record.my_level === "edit" || record.my_level === "manage";
  const email = record.type === "contact" ? record.emails[0] : undefined;
  const phone = record.type === "contact" ? record.phones[0] : record.type === "company" ? record.phone || undefined : undefined;
  const upcoming = crmUpcoming(timeline.data);
  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      refreshControl={
        <RefreshControl
          refreshing={rec.isRefetching}
          onRefresh={() => {
            void rec.refetch();
            void timeline.refetch();
          }}
        />
      }
    >
      <Stack.Screen
        options={{
          title: CRM_TYPE_LABEL[type].One,
          headerRight: () => <ResourceMenu feature="crm" kind={CRM_KIND[type]} id={record.id} title={record.title} orgId={orgId} />,
        }}
      />
      <View style={{ gap: s[3] }}>
        <Headline>{record.title}</Headline>
        <FactRow>
          {record.type === "contact" ? (
            <>
              {record.job_title ? <Text tone="muted">{record.job_title}</Text> : null}
              {record.company ? (
                <Text tone="primary" weight={600} onPress={() => router.push(recordPath("company", record.company?.id ?? ""))}>
                  {record.company.name}
                </Text>
              ) : null}
              <Badge>{CRM_LIFECYCLE_LABEL[record.lifecycle_stage]}</Badge>
            </>
          ) : record.type === "company" ? (
            <>
              {record.domain ? <Text tone="muted">{record.domain}</Text> : null}
              {record.industry ? <Text tone="muted">{record.industry}</Text> : null}
            </>
          ) : (
            <>
              <Text weight={600} tabular>
                {record.amount !== null ? crmMoney(record.amount, record.currency) : "No amount"}
              </Text>
              <StageBadge name={record.stage.name} outcome={record.stage.outcome} />
              {record.close_date ? <Text tone="muted">Closes {dayLabel(record.close_date)}</Text> : null}
            </>
          )}
        </FactRow>
        <Text role="caption" tone="muted">
          Owner: {record.owner?.name ?? "Unassigned"}
          {record.team ? ` · ${record.team.name}` : ""}
        </Text>
      </View>
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        {phone ? (
          <Button variant="secondary" size="md" leftIcon={Phone} onPress={() => void Linking.openURL(`tel:${phone.replace(/[^\d+]/g, "")}`)}>
            Call
          </Button>
        ) : null}
        {email ? (
          <Button variant="secondary" size="md" leftIcon={Mail} onPress={() => void Linking.openURL(`mailto:${email}`)}>
            Email
          </Button>
        ) : null}
        {canComment ? (
          <Button variant="secondary" size="md" leftIcon={NotebookPen} onPress={() => setLogging(true)}>
            Log
          </Button>
        ) : null}
        {record.type === "deal" && canEdit ? (
          <Button variant="secondary" size="md" leftIcon={ArrowRightLeft} onPress={() => setMoving(true)}>
            Stage
          </Button>
        ) : null}
      </View>
      <SegmentedControl<Pane>
        accessibilityLabel="Record sections"
        value={pane}
        onValueChange={setPane}
        options={[
          { value: "activity", label: "Activity" },
          { value: "about", label: "About" },
          { value: "related", label: "Related" },
        ]}
      />
      {pane === "activity" ? (
        <View style={{ gap: s[4] }}>
          {upcoming.length > 0 ? (
            <ListGroup title="Open tasks">
              {upcoming.map((t) => (
                <TaskRow key={t.id} task={t} />
              ))}
            </ListGroup>
          ) : null}
          {timeline.isPending ? (
            <Skeleton height={theme.tap.lg * 2} />
          ) : timeline.isError ? (
            <ErrorState compact title="Couldn't load the timeline" onRetry={() => void timeline.refetch()} />
          ) : timeline.data.items.length === 0 ? (
            <EmptyState compact icon={History} title="Nothing here yet" />
          ) : (
            timeline.data.items.map((it) =>
              it.activity ? <ActivityItem key={it.id} a={it.activity} record={record} /> : it.event ? <EventItem key={it.id} e={it.event} at={it.at} record={record} /> : null,
            )
          )}
          {timeline.hasNextPage ? (
            <Button variant="ghost" loading={timeline.isFetchingNextPage} onPress={() => void timeline.fetchNextPage()}>
              Load more
            </Button>
          ) : null}
        </View>
      ) : pane === "about" ? (
        <About record={record} />
      ) : (
        <Related associations={associations} type={type} />
      )}
      {canComment ? <LogSheet type={type} id={id} alsoOn={suggestions(record, associations)} open={logging} onClose={() => setLogging(false)} /> : null}
      {record.type === "deal" ? <StageSheet deal={record} orgId={orgId} open={moving} onClose={() => setMoving(false)} /> : null}
    </Screen>
  );
}

function who(name: string | undefined, agent: string | null) {
  return agent ? `${name ?? "Someone"} (via ${agent})` : (name ?? "Someone");
}

function ActivityItem({ a, record }: { a: CrmActivity; record: CrmRecord }) {
  const theme = useTheme();
  if (a.type === "task") {
    return (
      <ListGroup>
        <TaskRow task={a} />
      </ListGroup>
    );
  }
  const others = a.records.filter((r) => !(r.type === record.type && r.id === record.id));
  return (
    <Card padding="compact" style={{ gap: theme.space[2] }}>
      <Text role="label" weight={600}>
        {who(a.created_by?.name, a.agent)} · {CRM_ACTIVITY_LABEL[a.type]}
      </Text>
      <Text role="caption" tone="muted">
        {new Date(a.occurred_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
        {a.outcome ? ` · ${crmOutcomeLabel(a.outcome)}` : ""}
        {a.duration_minutes ? ` · ${a.duration_minutes} min` : ""}
      </Text>
      {a.subject ? <Text weight={600}>{a.subject}</Text> : null}
      {a.body ? (
        <Text>
          {crmBodySegments(a.body, a.mentions).map((seg, i) =>
            seg.kind === "text" ? (
              seg.text
            ) : (
              <Text key={i} tone="primary" weight={600}>
                @{seg.mention?.name ?? "someone"}
              </Text>
            ),
          )}
        </Text>
      ) : null}
      {others.length > 0 ? (
        <Text role="caption" tone="muted">
          Also on {others.map((r) => r.name).join(", ")}
        </Text>
      ) : null}
    </Card>
  );
}

function EventItem({ e, at, record }: { e: CrmEvent; at: string; record: CrmRecord }) {
  const theme = useTheme();
  const actor = who(e.actor?.name, e.agent);
  const d = e.data;
  let text = "";
  switch (e.kind) {
    case "created":
      text = `${actor} created this ${record.type}`;
      break;
    case "imported":
      text = `${actor} imported this ${record.type}`;
      break;
    case "merged":
      text = `${actor} merged “${d.merged_name}” into it`;
      break;
    case "stage_changed":
      text = `${actor} moved it to ${d.to_stage}${d.seconds_in_stage ? ` after ${crmDuration(d.seconds_in_stage)} in ${d.from_stage}` : ""}`;
      break;
    case "associated":
    case "dissociated":
      text = `${actor} ${e.kind === "associated" ? "linked" : "unlinked"} ${d.other_hidden ? `a ${d.other_type ?? "record"} you can't see` : `${d.other_type} ${d.other_name}`}`;
      break;
    case "updated":
      text = `${actor} changed ${(d.changes ?? []).map((c) => (c.after ? `${c.label} to ${c.after}` : `${c.label} (cleared)`)).join(", ")}`;
      break;
  }
  return (
    <View style={{ gap: theme.space[0.5], paddingHorizontal: theme.space[2] }}>
      <Text role="label">{text}</Text>
      <Text role="caption" tone="subtle">
        {dayLabel(at)}
      </Text>
    </View>
  );
}

function About({ record }: { record: CrmRecord }) {
  const rows: [string, string][] = [];
  const add = (label: string, v: string | null | undefined) => {
    if (v) rows.push([label, v]);
  };
  if (record.type === "contact") {
    add("Email", record.emails.join(", "));
    add("Phone", record.phones.join(", "));
    add("Job title", record.job_title);
    add("Lifecycle stage", CRM_LIFECYCLE_LABEL[record.lifecycle_stage]);
    add("City", record.city);
    add("Country", record.country);
  } else if (record.type === "company") {
    add("Domain", record.domain);
    add("Industry", record.industry);
    add("Employees", record.employees?.toLocaleString());
    add("Phone", record.phone);
    add("City", record.city);
    add("Country", record.country);
  } else {
    add("Amount", record.amount !== null ? crmMoney(record.amount, record.currency) : null);
    add("Weighted", record.weighted_amount !== null ? crmMoney(record.weighted_amount, record.currency) : null);
    add("Pipeline", record.pipeline_name);
    add("Stage", `${record.stage.name} · ${record.stage.probability}%`);
    add("Close date", record.close_date ? dayLabel(record.close_date) : null);
    add("Company", record.company?.name);
  }
  add("Owner", record.owner?.name);
  add("Team", record.team?.name);
  add("Tags", record.tags.map((t) => t.name).join(", "));
  return (
    <ListGroup>
      {rows.map(([label, v]) => (
        <ListRow
          key={label}
          title={label}
          trailing={
            <Text role="label" numberOfLines={2} style={{ maxWidth: 200, textAlign: "right" }}>
              {v}
            </Text>
          }
        />
      ))}
    </ListGroup>
  );
}

function Related({ associations: a, type }: { associations: CrmAssociations; type: CrmObjectType }) {
  const theme = useTheme();
  const groups: { t: CrmObjectType; title: string; rows: { id: string; name: string; detail: string }[] }[] = [];
  if (type !== "company") groups.push({ t: "company", title: "Companies", rows: a.companies.map((c) => ({ id: c.id, name: c.name, detail: c.is_primary ? "Primary" : (c.domain ?? "") })) });
  if (type !== "contact") groups.push({ t: "contact", title: "Contacts", rows: a.contacts.map((c) => ({ id: c.id, name: c.name, detail: c.job_title })) });
  if (type !== "deal") groups.push({ t: "deal", title: "Deals", rows: a.deals.map((d) => ({ id: d.id, name: d.name, detail: d.amount !== null ? crmMoney(d.amount, d.currency) : d.stage_name })) });
  return (
    <View style={{ gap: theme.space[4] }}>
      {groups.map((g) => {
        const Icon = TYPE_ICON[g.t];
        return (
          <ListGroup key={g.t} title={g.title}>
            {g.rows.length === 0 ? (
              <ListRow title="None you can see" />
            ) : (
              g.rows.map((r) => (
                <ListRow
                  key={r.id}
                  title={r.name}
                  leading={<Icon size={20} color={theme.colors.textMuted} />}
                  trailing={
                    r.detail ? (
                      <Text role="caption" tone="muted" numberOfLines={1} style={{ maxWidth: 120 }}>
                        {r.detail}
                      </Text>
                    ) : undefined
                  }
                  onPress={() => router.push(recordPath(g.t, r.id))}
                />
              ))
            )}
          </ListGroup>
        );
      })}
    </View>
  );
}

/** Move a deal to another stage of its pipeline (edit needed). */
export function StageSheet({ deal, orgId, open, onClose }: { deal: CrmDeal; orgId: string; open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const pipelines = useCrmPipelines(orgId);
  const move = useCrmMoveStage(orgId);
  const pl = pipelines.data?.find((p) => p.id === deal.pipeline_id);
  return (
    <Sheet open={open} onClose={onClose} title={`Move ${deal.name}`} snapPoints={["60%"]}>
      <ListGroup>
        {(pl?.stages ?? []).map((s) => (
          <ListRow
            key={s.id}
            title={`${s.name} · ${s.probability}%`}
            trailing={s.id === deal.stage.id ? <Check size={20} color={theme.colors.primaryStrong} /> : undefined}
            disabled={move.isPending}
            onPress={() => {
              if (s.id === deal.stage.id) return onClose();
              move.mutate(
                { deal, stageId: s.id },
                {
                  onSuccess: () => {
                    toast.success(`Moved to ${s.name}`);
                    onClose();
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't move the deal.")),
                },
              );
            }}
          />
        ))}
      </ListGroup>
    </Sheet>
  );
}
