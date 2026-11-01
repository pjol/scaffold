import { useSignIn, useSignUp } from "@clerk/expo";
import { DEV_IDENTITIES, useAuthSession, useDevAuth, type DevIdentityLike } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Display, Field, InfoTip, Input, ListGroup, ListRow, Screen, Spinner, enterUp, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { FlaskConical, KeyRound, Mail } from "lucide-react-native";
import { useRef, useState } from "react";
import { Platform, View, type TextInput } from "react-native";
import Animated from "react-native-reanimated";
import { LegalLinks } from "@/components/LegalLinks";
import { devIdentityForEmail } from "@/lib/auth";
import { appName } from "@/lib/version";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

/** Clerk email code + passkey in production; the seeded-identity picker when dev auth is on. */
export default function SignInScreen() {
  const session = useAuthSession();
  return session.mode === "dev" ? <DevSignIn /> : <ClerkSignIn />;
}

/** A code + message from whatever Clerk threw or returned. Clerk's long messages are user-facing. */
function clerkError(err: unknown): { code: string; message: string } {
  if (err && typeof err === "object") {
    const e = err as { code?: string; message?: string; longMessage?: string; errors?: { code?: string; message?: string; longMessage?: string }[] };
    const first = e.errors?.[0];
    return { code: first?.code ?? e.code ?? "unknown", message: first?.longMessage ?? first?.message ?? e.longMessage ?? e.message ?? "Couldn't sign in. Try again." };
  }
  return { code: "unknown", message: "Couldn't sign in. Try again." };
}

// ───────────────────────────────────────────────────────────── Clerk ──

type Step = "email" | "code";

/**
 * Custom Clerk flow (no web views): email → 6-digit code; an email Clerk doesn't know
 * becomes a sign-up verified by the same code. Passkeys sign in with the system sheet.
 * When the session becomes active the root gates move on (consent, then the app).
 */
function ClerkSignIn() {
  const theme = useTheme();
  const s = theme.space;
  const { signIn } = useSignIn();
  const { signUp } = useSignUp();
  const [step, setStep] = useState<Step>("email");
  const [flow, setFlow] = useState<"signIn" | "signUp">("signIn");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"code" | "verify" | "passkey" | null>(null);
  const codeRef = useRef<TextInput>(null);
  const emailOk = EMAIL_RE.test(email.trim());

  const fail = (e: unknown) => {
    haptic.error();
    setError(clerkError(e).message);
  };

  const sendCode = async () => {
    setError(null);
    setBusy("code");
    try {
      const res = await signIn.emailCode.sendCode({ emailAddress: email.trim() });
      if (res.error) {
        if (clerkError(res.error).code !== "form_identifier_not_found") throw res.error;
        const created = await signUp.create({ emailAddress: email.trim() });
        if (created.error) throw created.error;
        const sent = await signUp.verifications.sendEmailCode();
        if (sent.error) throw sent.error;
        setFlow("signUp");
      } else {
        setFlow("signIn");
      }
      setCode("");
      setStep("code");
      setTimeout(() => codeRef.current?.focus(), 300);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(null);
    }
  };

  const verify = async (value: string) => {
    setError(null);
    setBusy("verify");
    try {
      if (flow === "signIn") {
        const res = await signIn.emailCode.verifyCode({ code: value });
        if (res.error) throw res.error;
        if (signIn.status !== "complete") throw new Error("This account needs another sign-in step. Sign in on the web first.");
        const done = await signIn.finalize();
        if (done.error) throw done.error;
      } else {
        const res = await signUp.verifications.verifyEmailCode({ code: value });
        if (res.error) throw res.error;
        if (signUp.status !== "complete") throw new Error("Signing up needs more details than the app asks for. Sign up on the web first.");
        const done = await signUp.finalize();
        if (done.error) throw done.error;
      }
      haptic.success();
    } catch (e) {
      setCode("");
      fail(e);
    } finally {
      setBusy(null);
    }
  };

  const withPasskey = async () => {
    setError(null);
    setBusy("passkey");
    try {
      const res = await signIn.passkey({ flow: "discoverable" });
      if (res.error) throw res.error;
      const done = await signIn.finalize();
      if (done.error) throw done.error;
      haptic.success();
    } catch (e) {
      const { code: c } = clerkError(e);
      // Dismissing the system sheet is not an error worth showing.
      if (c !== "passkey_retrieval_cancelled") fail(e);
    } finally {
      setBusy(null);
    }
  };

  return (
    <Screen keyboard centered>
      <Animated.View entering={enterUp(theme)} style={{ gap: s[3] }}>
        <Display>{step === "code" ? "Check your email" : appName()}</Display>
        {step === "code" ? (
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Badge icon={Mail}>{email.trim()}</Badge>
            <InfoTip content="We sent a 6-digit code to this address." />
          </View>
        ) : null}
      </Animated.View>

      {step === "email" ? (
        <Animated.View key="email" entering={enterUp(theme, 1)} style={{ gap: s[4] }}>
          <Field label="Email" error={error}>
            <Input
              value={email}
              onChangeText={setEmail}
              placeholder="you@example.com"
              keyboardType="email-address"
              autoCapitalize="none"
              autoCorrect={false}
              autoComplete="email"
              textContentType="emailAddress"
              returnKeyType="go"
              onSubmitEditing={() => emailOk && void sendCode()}
              leadingIcon={Mail}
            />
          </Field>
          <View style={{ gap: s[3] }}>
            <Button fullWidth loading={busy === "code"} disabled={!emailOk || busy !== null} onPress={() => void sendCode()}>
              Email me a code
            </Button>
            {Platform.OS === "ios" || Platform.OS === "android" ? (
              <Button variant="secondary" fullWidth leftIcon={KeyRound} loading={busy === "passkey"} disabled={busy !== null} onPress={() => void withPasskey()}>
                Sign in with a passkey
              </Button>
            ) : null}
          </View>
        </Animated.View>
      ) : (
        <Animated.View key="code" entering={enterUp(theme, 1)} style={{ gap: s[4] }}>
          <Field label="Code" error={error}>
            <Input
              ref={codeRef}
              value={code}
              onChangeText={(t) => {
                const v = t.replace(/\D/g, "").slice(0, 6);
                setCode(v);
                if (v.length === 6) void verify(v);
              }}
              keyboardType="number-pad"
              textContentType="oneTimeCode"
              autoComplete="one-time-code"
              maxLength={6}
              style={{ letterSpacing: s[2], textAlign: "center", fontFamily: theme.font("body", 600) }}
            />
          </Field>
          <View style={{ gap: s[3] }}>
            <Button fullWidth loading={busy === "verify"} disabled={code.length !== 6 || busy !== null} onPress={() => void verify(code)}>
              Sign in
            </Button>
            <View style={{ flexDirection: "row", flexWrap: "wrap", justifyContent: "center", gap: s[2] }}>
              <Button variant="ghost" size="md" disabled={busy !== null} onPress={() => void sendCode()}>
                Send a new code
              </Button>
              <Button
                variant="ghost"
                size="md"
                disabled={busy !== null}
                onPress={() => {
                  setError(null);
                  setStep("email");
                }}
              >
                Use another email
              </Button>
            </View>
          </View>
        </Animated.View>
      )}
      <LegalLinks />
    </Screen>
  );
}

// ───────────────────────────────────────────────────────────── dev ──

/** Development sign-in: a seeded identity or any email, as `dev:<subject>:<email>` tokens. */
function DevSignIn() {
  const theme = useTheme();
  const s = theme.space;
  const dev = useDevAuth();
  const [busy, setBusy] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const emailOk = EMAIL_RE.test(email.trim());

  const pick = async (identity: DevIdentityLike) => {
    if (!dev) return;
    setBusy(identity.subject);
    try {
      await dev.signInAs(identity);
      haptic.success();
    } catch {
      haptic.error();
      toast.error("Couldn't save that identity. Try again.");
      setBusy(null);
    }
  };

  return (
    <Screen keyboard>
      <Animated.View entering={enterUp(theme)} style={{ gap: s[3] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2], flexWrap: "wrap" }}>
          <Display>Sign in</Display>
          <InfoTip size="md" content="Development sign-in. The API accepts these accounts only with APP_ENV=development and AUTH_DEV_BYPASS=true (dev-up sets both)." />
        </View>
        <Badge tone="warning" icon={FlaskConical}>
          Development
        </Badge>
      </Animated.View>

      <Animated.View entering={enterUp(theme, 1)}>
        <ListGroup title="Seeded accounts" info="Created by backend/cmd/seed with the Terms already accepted.">
          {DEV_IDENTITIES.map((identity) => (
            <ListRow
              key={identity.subject}
              title={identity.name}
              info={`${identity.info} ${identity.email}`}
              leading={<Avatar name={identity.name} size="md" />}
              trailing={busy === identity.subject ? <Spinner size={20} /> : identity.admin_role ? <Badge size="sm" tone="primary">Admin</Badge> : undefined}
              chevron={false}
              disabled={busy !== null}
              onPress={() => void pick(identity)}
            />
          ))}
        </ListGroup>
      </Animated.View>

      <Animated.View entering={enterUp(theme, 2)} style={{ gap: s[4] }}>
        <Field label="Any email" info="Creates the account on first sign-in. Use a .test address so no mail leaves this machine.">
          <Input
            value={email}
            onChangeText={setEmail}
            placeholder="name@scaffoldapp.test"
            keyboardType="email-address"
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="go"
            onSubmitEditing={() => emailOk && void pick(devIdentityForEmail(email))}
            leadingIcon={Mail}
          />
        </Field>
        <Button variant="secondary" fullWidth disabled={!emailOk || busy !== null} loading={busy !== null && busy === devIdentityForEmail(email).subject} onPress={() => void pick(devIdentityForEmail(email))}>
          Sign in with this email
        </Button>
      </Animated.View>
      <LegalLinks />
    </Screen>
  );
}
