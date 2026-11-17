import { LineChart } from "lucide-react-native";
import type { HomeEntry } from "../registry";

/** Home → Prices: every listed feed with its published price. */
export const oracleHome: HomeEntry = { id: "oracle", title: "Prices", icon: LineChart, href: "/oracle" };
