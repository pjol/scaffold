import { Gauge } from "lucide-react-native";
import type { HomeEntry } from "../registry";

/** Home → Reputation: your score explained. */
export const reputationHome: HomeEntry = { id: "reputation", title: "Reputation", icon: Gauge, href: "/reputation" };
