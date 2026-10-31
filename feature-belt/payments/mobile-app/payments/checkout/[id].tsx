import { formatMinor, isPaymentsUnconfigured, useCheckoutStatus } from "@scaffoldapp/shared";
import { Button, Celebrate, EmptyState, ErrorState, Screen, Spinner, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { CircleCheck, CircleSlash, Clock } from "lucide-react-native";
import { View } from "react-native";
import { PaymentsUnavailable } from "@/features/payments/PaymentsUnavailable";

/**
 * After the in-app browser closes: the checkout's real status, polled until the
 * provider's webhook confirms it. Closing checkout without paying leaves it pending,
 * so pending says so plainly instead of claiming success.
 */
export default function CheckoutStatusScreen() {
  const theme = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const status = useCheckoutStatus(id ?? null);
  const s = status.data;

  const done = (
    <Button variant="secondary" onPress={() => router.dismissTo("/payments/billing")}>
      Go to billing
    </Button>
  );

  let body;
  if (status.isError) {
    body = isPaymentsUnconfigured(status.error) ? (
      <PaymentsUnavailable />
    ) : (
      <ErrorState title="Couldn't check your payment" info={status.error.message} onRetry={() => void status.refetch()} retrying={status.isRefetching} />
    );
  } else if (!s) {
    body = <Spinner size={32} label="Checking your payment" />;
  } else if (s.status === "completed") {
    body = (
      <View style={{ alignItems: "center", gap: theme.space[6] }}>
        <Celebrate fire={s.id}>
          <CircleCheck size={64} color={theme.colors.successStrong} strokeWidth={1.75} />
        </Celebrate>
        <View style={{ alignItems: "center", gap: theme.space[2] }}>
          <Title align="center">Payment received</Title>
          {s.amount_minor !== null ? (
            <Text tone="muted" tabular>
              {formatMinor(s.amount_minor, s.currency)}
            </Text>
          ) : null}
        </View>
        {done}
      </View>
    );
  } else if (s.status === "pending") {
    body = (
      <EmptyState
        icon={Clock}
        title="Not confirmed yet"
        info="If you finished paying, this updates within a minute. If you closed checkout, nothing was charged."
        action={done}
      />
    );
  } else {
    body = (
      <EmptyState
        icon={CircleSlash}
        title="The payment didn't go through"
        info={s.status === "expired" ? "The checkout page expired before payment." : "The checkout couldn't be completed. If you were charged, contact support and it will be refunded."}
        action={
          <Button variant="secondary" onPress={() => router.back()}>
            Back to plans
          </Button>
        }
      />
    );
  }

  return (
    <Screen edges={["bottom"]} centered>
      <Stack.Screen options={{ title: "Payment" }} />
      {body}
    </Screen>
  );
}
