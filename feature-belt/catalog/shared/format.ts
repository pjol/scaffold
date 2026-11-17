/** A reference price with its currency ("$4,000.00", "€0.10"); "" without a price. */
export function formatCatalogPrice(price: string | null | undefined, currency: string): string {
  if (!price) return "";
  const n = Number(price);
  if (!Number.isFinite(n)) return `${price} ${currency}`.trim();
  try {
    return new Intl.NumberFormat("en-US", { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: Math.max(2, decimals(price)) }).format(n);
  } catch {
    return `${price} ${currency}`.trim();
  }
}

function decimals(price: string): number {
  const dot = price.indexOf(".");
  return dot < 0 ? 0 : Math.min(price.length - dot - 1, 8);
}

/** Attribute names as labels: "atomic_number" → "Atomic number". */
export function catalogAttrLabel(name: string): string {
  const words = name.replace(/[_-]+/g, " ").replace(/([a-z])([A-Z])/g, "$1 $2").trim().toLowerCase();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : name;
}

/** Initials for an item without an image: up to 2 letters of its name. */
export function catalogMonogram(name: string): string {
  const words = name.match(/[\p{L}\p{N}]+/gu) ?? [];
  const letters = words.length > 1 ? (words[0]?.charAt(0) ?? "") + (words[1]?.charAt(0) ?? "") : (words[0] ?? "").slice(0, 2);
  return letters.toUpperCase();
}
