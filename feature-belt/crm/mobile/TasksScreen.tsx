import { crmIsOverdue, errorMessage, useCrmTaskSummary, useCrmTasks, useCrmUpdateActivity, type CrmActivity, type CrmTaskBucket } from "@scaffoldapp/shared";
import { Button, Checkbox, EmptyState, ErrorState, ListGroup, Screen, SegmentedControl, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CheckSquare } from "lucide-react-native";
import { useState } from "react";
import { Pressable, RefreshControl, View } from "react-native";
import { CrmGate, dayLabel, recordPath, timeLabel } from "./parts";

/** My tasks on mobile: overdue, today, upcoming, done. */
export function CrmTasksScreen() {
  return <CrmGate>{({ orgId }) => <Tasks orgId={orgId} />}</CrmGate>;
}

function Tasks({ orgId }: { orgId: string }) {
  const theme = useTheme();
  const [bucket, setBucket] = useState<CrmTaskBucket>("today");
  const summary = useCrmTaskSummary(orgId);
  const tasks = useCrmTasks(orgId, bucket);
  const n = (b: "overdue" | "today" | "upcoming") => (summary.data?.[b] ? ` ${summary.data[b]}` : "");
  return (
    <Screen edges={["bottom"]} gap={theme.space[4]} refreshControl={<RefreshControl refreshing={tasks.isRefetching} onRefresh={() => void tasks.refetch()} />}>
      <Stack.Screen options={{ title: "My tasks" }} />
      <SegmentedControl<CrmTaskBucket>
        accessibilityLabel="When"
        value={bucket}
        onValueChange={setBucket}
        options={[
          { value: "overdue", label: `Late${n("overdue")}` },
          { value: "today", label: `Today${n("today")}` },
          { value: "upcoming", label: "Later" },
          { value: "done", label: "Done" },
        ]}
      />
      <ListGroup>
        {tasks.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
            <SkeletonRow />
          </View>
        ) : tasks.isError ? (
          <ErrorState compact title="Couldn't load tasks" info={tasks.error.message} onRetry={() => void tasks.refetch()} retrying={tasks.isRefetching} />
        ) : tasks.data.items.length === 0 ? (
          <EmptyState compact icon={CheckSquare} title={bucket === "done" ? "No finished tasks yet" : bucket === "overdue" ? "Nothing overdue" : bucket === "today" ? "Nothing due today" : "No upcoming tasks"} />
        ) : (
          tasks.data.items.map((t) => <TaskRow key={t.id} task={t} />)
        )}
      </ListGroup>
      {tasks.hasNextPage ? (
        <Button variant="ghost" loading={tasks.isFetchingNextPage} onPress={() => void tasks.fetchNextPage()}>
          Load more
        </Button>
      ) : null}
    </Screen>
  );
}

/** A task: done checkbox, title (opens its record), record name and due time (late in danger). */
export function TaskRow({ task }: { task: CrmActivity }) {
  const theme = useTheme();
  const update = useCrmUpdateActivity(task.org_id);
  const done = !!task.done_at;
  const late = crmIsOverdue(task);
  const rec = task.records[0];
  return (
    <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[3], paddingHorizontal: theme.space[4], paddingVertical: theme.space[2], minHeight: theme.tap.lg }}>
      <Checkbox
        accessibilityLabel={done ? `Reopen ${task.subject}` : `Mark ${task.subject} done`}
        checked={done}
        disabled={update.isPending}
        onCheckedChange={(v) =>
          update.mutate({ id: task.id, done: v }, { onSuccess: () => toast.success(v ? "Task done" : "Task reopened"), onError: (e) => toast.error(errorMessage(e, "Couldn't update the task.")) })
        }
      />
      <Pressable style={{ flex: 1, gap: theme.space[0.5] }} accessibilityRole="button" onPress={() => rec && router.push(recordPath(rec.type, rec.id))}>
        <Text weight={600} tone={done ? "muted" : "default"} style={done ? { textDecorationLine: "line-through" } : undefined}>
          {task.subject}
        </Text>
        <Text role="caption" tone={late ? "danger" : "muted"} numberOfLines={1}>
          {[rec?.name, task.due_at ? `${dayLabel(task.due_at)} ${timeLabel(task.due_at)}` : "No due date", task.assignee ? task.assignee.name : ""].filter(Boolean).join(" · ")}
        </Text>
      </Pressable>
    </View>
  );
}
