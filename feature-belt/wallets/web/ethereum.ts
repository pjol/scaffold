/**
 * The browser wallet (an injected EIP-1193 provider at `window.ethereum`: MetaMask,
 * Rabby, Coinbase Wallet, Brave). Only what linking needs: accounts, chain id and
 * personal_sign. No keys or transactions here.
 */

interface Eip1193Provider {
  request(args: { method: string; params?: unknown[] }): Promise<unknown>;
}

interface ProviderError {
  code?: number;
  message?: string;
}

/** The injected provider, or null (no wallet extension, or server render). */
export function injectedProvider(): Eip1193Provider | null {
  if (typeof window === "undefined") return null;
  const eth = (window as unknown as { ethereum?: Eip1193Provider }).ethereum;
  return eth && typeof eth.request === "function" ? eth : null;
}

function provider(): Eip1193Provider {
  const p = injectedProvider();
  if (!p) throw new Error("No browser wallet found. Install a wallet extension, then reload this page.");
  return p;
}

/** Asks the wallet to connect; resolves to its addresses (the selected one first). */
export async function requestAccounts(): Promise<string[]> {
  const accounts = await provider().request({ method: "eth_requestAccounts" });
  return Array.isArray(accounts) ? accounts.filter((a): a is string => typeof a === "string") : [];
}

/** The chain the wallet is on, as a number (null when it doesn't say). */
export async function walletChainId(): Promise<number | null> {
  const hex = await provider().request({ method: "eth_chainId" });
  if (typeof hex !== "string") return null;
  const n = Number.parseInt(hex, 16);
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/** personal_sign of a UTF-8 message (sent hex-encoded, as wallets expect). */
export async function personalSign(message: string, address: string): Promise<string> {
  const bytes = new TextEncoder().encode(message);
  let hex = "0x";
  for (const b of bytes) hex += b.toString(16).padStart(2, "0");
  const sig = await provider().request({ method: "personal_sign", params: [hex, address] });
  if (typeof sig !== "string" || !sig.startsWith("0x")) throw new Error("Your wallet returned no signature. Try again.");
  return sig;
}

/** User-facing copy for a wallet (EIP-1193) error, or null when it isn't one. */
export function walletErrorMessage(error: unknown): string | null {
  const e = error as ProviderError | null;
  if (!e || typeof e !== "object" || typeof e.code !== "number") return null;
  switch (e.code) {
    case 4001:
      return "You declined the request in your wallet.";
    case -32002:
      return "Your wallet already has a request open. Finish it there first.";
    case 4100:
      return "Your wallet hasn't allowed this site. Connect it, then try again.";
    default:
      return "Your wallet couldn't complete the request. Try again.";
  }
}
