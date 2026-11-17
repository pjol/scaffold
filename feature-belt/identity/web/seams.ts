/**
 * Client seams an app sets from its stitching, once at startup and outside React.
 *
 * Paid badge link: where "Get the … badge" in Settings → Verification goes, given the
 * entitlement that grants the badge (`IDENTITY_PAID_ENTITLEMENT`, sent by the server). With the
 * payments feature: `setIdentityPaidBadgeLink((e) => `/payments?entitlement=${e}`)`. Unset, the
 * paid badge section shows only to people who hold or held the badge.
 */
export type IdentityPaidBadgeLinker = (entitlement: string) => string | null;

let paidBadgeLinker: IdentityPaidBadgeLinker | null = null;

/** Sets where "Get the paid badge" goes; null hides it. */
export function setIdentityPaidBadgeLink(linker: IdentityPaidBadgeLinker | null): void {
  paidBadgeLinker = linker;
}

/** The app path that sells the paid badge, or null when the app sells none. */
export function identityPaidBadgeLink(entitlement: string): string | null {
  return paidBadgeLinker ? paidBadgeLinker(entitlement) : null;
}
