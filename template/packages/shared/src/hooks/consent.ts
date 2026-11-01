"use client";

import { useMutation, useQueryClient, type QueryClient, type UseMutationOptions, type UseMutationResult } from "@tanstack/react-query";
import type { ApiClient } from "../api/client";
import { isConflict, type ApiError } from "../api/errors";
import { PRIVACY_VERSION, TERMS_VERSION } from "../legal/versions";
import type { AcceptConsentsRequest, Me } from "../types";
import { useMe } from "./common";
import { useApi } from "./context";
import { queryKeys } from "./keys";

/** What the app needs to decide whether to show the consent screen. */
export interface ConsentGate {
  /** Show the consent screen (`Me.consent.required`). False while loading or signed out. */
  required: boolean;
  /** Versions the server requires; null until `me` loads. */
  termsVersion: string | null;
  privacyVersion: string | null;
  /** `me` is loading for the first time. */
  loading: boolean;
  /**
   * The documents bundled in this build (`TERMS_VERSION` / `PRIVACY_VERSION`) are the
   * ones the server requires. False = this build is out of step with the server: show
   * the bundled documents anyway and prompt an app update.
   */
  bundledVersionsMatch: boolean;
  error: ApiError | null;
}

/** Pure gate computation (exported for tests). */
export function consentGateState(me: Pick<Me, "consent"> | undefined, status: { loading: boolean; error?: ApiError | null }): ConsentGate {
  const termsVersion = me?.consent.terms_version ?? null;
  const privacyVersion = me?.consent.privacy_version ?? null;
  return {
    required: !!me?.consent.required,
    termsVersion,
    privacyVersion,
    loading: status.loading,
    bundledVersionsMatch: !me || (termsVersion === TERMS_VERSION && privacyVersion === PRIVACY_VERSION),
    error: status.error ?? null,
  };
}

/**
 * Consent gate from `useMe`. Render the consent screen when `required`. Any
 * `consent_required` API error refetches `me` (via `ApiProvider`), so the gate also
 * catches a policy bump mid-session.
 *
 * ```tsx
 * const gate = useConsentGate({ enabled: session.isSignedIn });
 * if (gate.required) return <ConsentScreen />;
 * ```
 */
export function useConsentGate(options: { enabled?: boolean } = {}): ConsentGate {
  const enabled = options.enabled ?? true;
  const me = useMe({ enabled });
  return consentGateState(enabled ? me.data : undefined, {
    loading: enabled && me.isPending && me.fetchStatus !== "idle",
    error: me.error ?? null,
  });
}

/** Variables for {@link useAcceptConsents}. Versions default to the ones `me` reports as current. */
export interface AcceptConsentsInput {
  terms_version?: string;
  privacy_version?: string;
}

/** Mutation options for {@link useAcceptConsents} (exported for tests). */
export function acceptConsentsMutation(api: ApiClient, qc: QueryClient): UseMutationOptions<Me, ApiError, AcceptConsentsInput | void> {
  return {
    mutationFn: async (input) => {
      const current = qc.getQueryData<Me>(queryKeys.me())?.consent;
      const body: AcceptConsentsRequest = {
        terms_version: input?.terms_version ?? current?.terms_version ?? TERMS_VERSION,
        privacy_version: input?.privacy_version ?? current?.privacy_version ?? PRIVACY_VERSION,
        platform: api.platform,
      };
      return api.acceptConsents(body);
    },
    onSuccess: (me) => {
      qc.setQueryData(queryKeys.me(), me);
    },
    onError: (error) => {
      // 409: the versions moved while the user was reading. Refresh what the screen shows.
      if (isConflict(error)) void qc.invalidateQueries({ queryKey: queryKeys.config() });
      void qc.invalidateQueries({ queryKey: queryKeys.me() });
    },
  };
}

/**
 * `POST /v1/me/consents`: accept the current Terms and Privacy versions; `platform` comes
 * from the API client. Updates the `me` cache, which clears the gate.
 */
export function useAcceptConsents(): UseMutationResult<Me, ApiError, AcceptConsentsInput | void> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation(acceptConsentsMutation(api, qc));
}
