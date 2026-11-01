import { errorMessage, formatTokenCount, useAssistantStatus, useWithdrawAssistantConsent } from "@scaffoldapp/shared";
import { Body, ConfirmDialog, ErrorState, ListGroup, ListRow, Screen, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { useState } from "react";
import { View } from "react-native";
import { ConsentDialog } from "./ConsentDialog";

const date = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });
const dateTime = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" });

/** Settings → Assistant: which provider receives your messages, your consent, today's usage. */
export function AssistantSettingsScreen() {
  const theme = useTheme();
  const status = useAssistantStatus();
  const withdraw = useWithdrawAssistantConsent();
  const [allowing, setAllowing] = useState(false);
  const [withdrawing, setWithdrawing] = useState(false);

  if (status.isPending) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Skeleton height={theme.tap.base * 2} />
        <Skeleton height={theme.tap.base * 3} />
      </Screen>
    );
  }
  if (status.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <ErrorState title="Couldn't load assistant settings" onRetry={() => void status.refetch()} retrying={status.isFetching} />
      </Screen>
    );
  }
  const st = status.data;
  const u = st.usage;
  const value = (v: string) => (
    <Text tone="muted" tabular>
      {v}
    </Text>
  );
  return (
    <Screen edges={["bottom"]} gap={theme.space[6]}>
      <ListGroup title="AI provider" info="Your messages, and the app data the assistant reads to answer them, are sent to this company to write replies.">
        <ListRow title="Provider" chevron={false} trailing={value(st.provider?.label ?? "Not configured")} />
        {st.provider ? (
          st.consent.required ? (
            <ListRow title="Allow the assistant" chevron={false} onPress={() => setAllowing(true)} />
          ) : (
            <ListRow title="Turn off the assistant" destructive chevron={false} trailing={value(`Allowed ${date.format(new Date(st.consent.accepted_at ?? Date.now()))}`)} onPress={() => setWithdrawing(true)} />
          )
        ) : null}
      </ListGroup>
      <ListGroup title="Today" info="Limits reset at midnight UTC.">
        <ListRow title="Messages" chevron={false} trailing={value(u.unlimited || u.message_limit === 0 ? String(u.messages) : `${u.messages} of ${u.message_limit}`)} />
        <ListRow
          title="Tokens"
          chevron={false}
          trailing={value(u.unlimited || u.token_limit === 0 ? formatTokenCount(u.tokens) : `${formatTokenCount(u.tokens)} of ${formatTokenCount(u.token_limit)}`)}
        />
        <ListRow title="Resets" chevron={false} trailing={value(dateTime.format(new Date(u.resets_at)))} />
      </ListGroup>
      {st.provider ? <ConsentDialog provider={st.provider} open={allowing} onClose={() => setAllowing(false)} onAccepted={() => toast.success("Assistant turned on")} /> : null}
      <ConfirmDialog
        open={withdrawing}
        onClose={() => setWithdrawing(false)}
        title="Turn off the assistant?"
        confirmLabel="Turn off"
        tone="danger"
        onConfirm={async () => {
          await withdraw.mutateAsync();
          toast.success("Assistant turned off");
        }}
        errorText={(e) => errorMessage(e, "Couldn't save your choice. Try again.")}
      >
        <View>
          <Body>{`Nothing more is sent to ${st.provider?.label ?? "the provider"} until you allow it again. Your conversations stay until you delete them.`}</Body>
        </View>
      </ConfirmDialog>
    </Screen>
  );
}
