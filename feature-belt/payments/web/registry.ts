import { CreditCard, Wallet } from "lucide-react";
import type { NavItem, SettingsSection } from "@/features/registry";

/** Settings → Billing (plan, portal link, history). */
export const paymentsSettings: SettingsSection = { href: "/settings/billing", label: "Billing", icon: CreditCard };

/** Admin → Payments (customers, ledger, refunds, prices). */
export const paymentsAdminNav: NavItem = { href: "/admin/payments", label: "Payments", icon: Wallet };
