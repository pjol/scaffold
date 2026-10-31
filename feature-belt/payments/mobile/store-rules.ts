import * as WebBrowser from "expo-web-browser";

/**
 * THE STORE RULE (Apple 3.1.1 / 3.1.3, Google Play Billing). Read before turning this on.
 *
 * - Digital content or features used in the app (a premium tier, credits, extra features):
 *   iOS and Android require Apple In-App Purchase / Google Play Billing. Opening Stripe
 *   Checkout or the Stripe billing portal for these inside the app gets the app
 *   rejected. This module does not do IAP; use RevenueCat or StoreKit/Play Billing.
 * - Physical goods, real-world services, bookings, tickets to in-person events,
 *   donations to registered charities: Stripe is required and IAP is not allowed.
 * - US storefront only: a link out to web checkout for digital goods is allowed
 *   (re-check; fees are contested). Other regions need their own entitlements.
 *
 * So the app opens hosted checkout and the billing portal only when the project has
 * decided its catalog is physical goods or services and set
 * EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT=on. Off (the default), the app shows plan
 * status and history, and purchases happen on the web.
 */
export const MOBILE_CHECKOUT_ENABLED = process.env.EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT === "on";

/**
 * Open a hosted payments page (checkout, portal) in the in-app browser. Resolves when
 * the user closes it. Callers check MOBILE_CHECKOUT_ENABLED first.
 */
export async function openHostedPage(url: string): Promise<void> {
  if (!MOBILE_CHECKOUT_ENABLED) throw new Error("Hosted payment pages are off in this app (EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT).");
  await WebBrowser.openBrowserAsync(url, {
    presentationStyle: WebBrowser.WebBrowserPresentationStyle.PAGE_SHEET,
    dismissButtonStyle: "done",
    createTask: false,
  });
}
