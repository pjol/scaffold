import { EmptyState } from "@scaffoldapp/ui-native";
import { CreditCard } from "lucide-react-native";

/** 503 `payments_unconfigured`: the server has no payment provider. Nothing pretends to work. */
export function PaymentsUnavailable({ compact }: { compact?: boolean }) {
  return <EmptyState compact={compact} icon={CreditCard} title="Payments aren't set up yet" info="The server has no payment provider configured. Billing appears here once it does." />;
}
