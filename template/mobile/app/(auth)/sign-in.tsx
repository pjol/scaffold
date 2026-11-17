import { useSignIn, useSignUp } from "@clerk/expo";
import { DEV_IDENTITIES, useAuthSession, useDevAuth, type DevIdentityLike } from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Caption, Divider, Field, InfoTip, Input, ListGroup, ListRow, Screen, Spinner, Title, enterUp, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { KeyRound, Mail } from "lucide-react-native";
import { useRef, useState, type ReactNode } from "react";
import { Platform, View, type TextInput } from "react-native";
import Animated from "react-native-reanimated";
import { LegalLinks } from "@/components/LegalLinks";
import { devIdentityForEmail } from "@/lib/auth";
import { appName } from "@/lib/version";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

/** Clerk email code + passkey in production; email or a seeded account when dev auth is on. Laid out like the web sign-in. */
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

/** The shared frame: centred heading, the form, "or" + the alternatives, legal links. */
function SignInLayout({ heading, children, alternatives }: { heading: ReactNode; children: ReactNode; alternatives?: ReactNode }) {
  const s = useTheme().space;
  return (
    <Screen keyboard centered gap={s[6]}>
      {heading}
      {children}
      {alternatives ? (
        <>
          <OrDivider />
          {alternatives}
        </>
      ) : null}
      <LegalLinks />
    </Screen>
  );
}

function OrDivider() {
  const s = useTheme().space;
  return (
    <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
      <Divider style={{ flex: 1 }} />
      <Caption>or</Caption>
      <Divider style={{ flex: 1 }} />
    </View>
  );
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
  const passkeys = Platform.OS === "ios" || Platform.OS === "android";

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

  if (step === "code") {
    return (
      <SignInLayout
        heading={
          <View style={{ alignItems: "center", gap: s[3] }}>
            <Title align="center">Check your email</Title>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Badge icon={Mail}>{email.trim()}</Badge>
              <InfoTip content="We sent a 6-digit code to this address." />
            </View>
          </View>
        }
      >
        <Animated.View key="code" entering={enterUp(theme)} style={{ gap: s[3] }}>
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
        </Animated.View>
      </SignInLayout>
    );
  }

  return (
    <SignInLayout
      heading={<Title align="center">Sign in to {appName()}</Title>}
      alternatives={
        passkeys ? (
          <Button variant="secondary" fullWidth leftIcon={KeyRound} loading={busy === "passkey"} disabled={busy !== null} onPress={() => void withPasskey()}>
            Sign in with a passkey
          </Button>
        ) : undefined
      }
    >
      <Animated.View key="email" entering={enterUp(theme)} style={{ gap: s[3] }}>
        <Field label="Email address" error={error}>
          <Input
            value={email}
            onChangeText={setEmail}
            placeholder="name@work-email.com"
            keyboardType="email-address"
            autoCapitalize="none"
            autoCorrect={false}
            autoComplete="email"
            textContentType="emailAddress"
            returnKeyType="go"
            onSubmitEditing={() => emailOk && void sendCode()}
          />
        </Field>
        <Button fullWidth loading={busy === "code"} disabled={!emailOk || busy !== null} onPress={() => void sendCode()}>
          Continue
        </Button>
      </Animated.View>
    </SignInLayout>
  );
}

// ───────────────────────────────────────────────────────────── dev ──

/** Development sign-in: any email or a seeded account, as `dev:<subject>:<email>` tokens. */
function DevSignIn() {
  const s = useTheme().space;
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
    <SignInLayout
      heading={<Title align="center">Sign in to {appName()}</Title>}
      alternatives={
        <ListGroup
          title="Development accounts"
          info="Development builds sign in without a password: pick a seeded account (created by backend/cmd/seed, Terms accepted) or type any email (a new address creates its account; use .test so no mail leaves this machine). The API accepts these only with APP_ENV=development and AUTH_DEV_BYPASS=true, which dev-up sets."
        >
          {DEV_IDENTITIES.map((identity) => (
            <ListRow
              key={identity.subject}
              title={identity.name}
              leading={<Avatar name={identity.name} size="sm" />}
              trailing={busy === identity.subject ? <Spinner size={20} /> : identity.admin_role ? <Badge size="sm" tone="primary">Admin</Badge> : undefined}
              disabled={busy !== null}
              onPress={() => void pick(identity)}
            />
          ))}
        </ListGroup>
      }
    >
      <View style={{ gap: s[3] }}>
        <Field label="Email address">
          <Input
            value={email}
            onChangeText={setEmail}
            placeholder="name@work-email.com"
            keyboardType="email-address"
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="go"
            onSubmitEditing={() => emailOk && void pick(devIdentityForEmail(email))}
          />
        </Field>
        <Button fullWidth disabled={!emailOk || busy !== null} loading={busy !== null && busy === devIdentityForEmail(email).subject} onPress={() => void pick(devIdentityForEmail(email))}>
          Continue
        </Button>
      </View>
    </SignInLayout>
  );
}
