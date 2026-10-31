import { CreditCard } from "lucide-react-native";
import type { SettingsSection } from "@/features/registry";

/** Settings → Billing (plan status and payment history). */
export const paymentsSettings: SettingsSection = {
  id: "payments",
  items: [{ id: "billing", title: "Billing", icon: CreditCard, href: "/payments/billing" }],
};
