import { errorMessage, isNotFound, useAgentsSuggestion } from "@scaffoldapp/shared";
import { Badge, Banner, Button, EmptyState, ErrorState, InfoTip, ListGroup, ListRow, Screen, SkeletonRow, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams, type Href } from "expo-router";
import { ExternalLink, SearchX } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { dateTime, STATUS_LABEL, STATUS_TONE, VIA_LABEL } from "./labels";
import { SuggestionChange, SuggestionDiff } from "./SuggestionDiff";
import { useSuggestionDecision } from "./SuggestionsScreen";

/** One suggestion: what changes, where, who suggested it, the outcome; Accept and Reject in the footer. */
export function AgentsSuggestionScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { id } = useLocalSearchParams<{ id: string }>();
  const q = useAgentsSuggestion(id);
  const d = useSuggestionDecision();
  const [changed, setChanged] = useState<{ before: string; after: string } | null>(null);
  const sg = q.data;
  const pending = sg?.status === "pending";

  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      footer={
        sg && pending ? (
          <View style={{ flexDirection: "row", gap: s[2] }}>
            <Button variant="ghost" style={{ flex: 1 }} disabled={d.busy} loading={d.rejecting} onPress={() => d.reject(sg)}>
              Reject
            </Button>
            <Button style={{ flex: 1 }} disabled={d.busy} loading={d.accepting} onPress={() => d.accept(sg, !!changed, setChanged)}>
              {changed ? "Accept anyway" : "Accept"}
            </Button>
          </View>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: "Suggestion" }} />
      {q.isPending ? (
        <ListGroup>
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </ListGroup>
      ) : q.isError ? (
        isNotFound(q.error) ? (
          <EmptyState icon={SearchX} title="Suggestion not found" />
        ) : (
          <ErrorState title="Couldn't load this suggestion" info={errorMessage(q.error)} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        )
      ) : sg ? (
        <>
          <View style={{ gap: s[3] }}>
            <View style={{ flexDirection: "row", gap: s[2], alignItems: "center" }}>
              <Badge tone={STATUS_TONE[sg.status]}>{STATUS_LABEL[sg.status]}</Badge>
            </View>
            <Text role="title">{sg.summary}</Text>
          </View>

          {sg.status === "accepted" ? <Banner tone="success" title="Done. The change was made as you." /> : null}
          {sg.status === "accepting" ? <Banner tone="info" title="Applying this change now." /> : null}
          {sg.status === "rejected" ? <Banner tone="primary" title="You rejected this. Nothing was changed." /> : null}
          {(sg.status === "failed" || sg.status === "expired") && sg.error ? <Banner tone={sg.status === "failed" ? "danger" : "primary"} title={sg.error.message} /> : null}
          {changed ? <Banner tone="warning" title={`This changed since ${sg.agent.client} suggested it. Here is the change against the current version.`} /> : null}

          <View style={{ gap: s[3] }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Text role="titleSm">{sg.tool_title}</Text>
              <InfoTip content={`${sg.agent.client} called ${sg.tool}${sg.source ? ` on ${sg.source}` : ""}. Accepting runs it as you, so your access now decides whether it works.`} />
            </View>
            {changed ? <SuggestionDiff before={changed.before} after={changed.after} /> : <SuggestionChange s={sg} />}
          </View>

          <ListGroup>
            <ListRow title="Agent" info={VIA_LABEL[sg.agent.via] ?? sg.agent.via} trailing={<Text tone="muted">{sg.agent.client}</Text>} />
            <ListRow title="Where" trailing={<Text tone="muted">{sg.feature_label}</Text>} />
            <ListRow title="Suggested" trailing={<Text tone="muted">{dateTime(sg.created_at)}</Text>} />
            {sg.decided_at ? (
              <ListRow title={sg.status === "expired" ? "Expired" : "Decided"} trailing={<Text tone="muted">{dateTime(sg.decided_at)}</Text>} />
            ) : (
              <ListRow title="Expires" trailing={<Text tone="muted">{dateTime(sg.expires_at)}</Text>} />
            )}
            {sg.link ? <ListRow title="Open" leading={<ExternalLink size={20} color={theme.colors.text} />} onPress={() => router.push(sg.link as Href)} /> : null}
          </ListGroup>
        </>
      ) : null}
    </Screen>
  );
}
