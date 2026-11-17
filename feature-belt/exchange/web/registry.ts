import { CandlestickChart, WalletCards } from "lucide-react";
import type { HomeTile, NavItem, SessionMount } from "../registry";
import { ExchangeToasts } from "./ExchangeToasts";

/** Main nav: the markets list (trade screens are under it). */
export const exchangeNav: NavItem = { href: "/markets", label: "Markets", icon: CandlestickChart };

/** Main nav: the trading account. */
export const exchangePortfolioNav: NavItem = { href: "/portfolio", label: "Portfolio", icon: WalletCards };

/** Home tiles (separate names: `scaffold add` skips lines already in the registry). */
export const exchangeHomeTile: HomeTile = { href: "/markets", label: "Markets", icon: CandlestickChart };
export const exchangePortfolioTile: HomeTile = { href: "/portfolio", label: "Portfolio", icon: WalletCards };

/** Admin tab: the catalog, market status, manual prices and exchange totals. */
export const exchangeAdminNav: NavItem = { href: "/admin/exchange", label: "Exchange", icon: CandlestickChart };

/** Signed-in toasts for liquidations and settled transfers. */
export const exchangeSession: SessionMount = ExchangeToasts;
