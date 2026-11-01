import { errorMessage, useConfig, useIdentityStatus, useStartVerification, useSubmitIdentityDocuments, useUpload, type IdentityStatus } from "@scaffoldapp/shared";
import { Banner, Button, Card, ErrorState, Field, InfoTip, PhotoUploader, Screen, Skeleton, TitleSm, haptic, toast, useTheme, type UploadedPhoto } from "@scaffoldapp/ui-native";
import * as Linking from "expo-linking";
import * as WebBrowser from "expo-web-browser";
import { BadgeCheck, Clock, ShieldAlert, ShieldCheck, ShieldQuestion } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { View } from "react-native";

function dateText(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

function StateCard({ icon, title, info, children }: { icon: ReactNode; title: string; info?: string; children?: ReactNode }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <Card>
      <View style={{ gap: s[4] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
          <View style={{ width: s[10], height: s[10], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken, alignItems: "center", justifyContent: "center" }}>{icon}</View>
          <TitleSm style={{ flex: 1 }}>{title}</TitleSm>
          {info ? <InfoTip content={info} /> : null}
        </View>
        {children}
      </View>
    </Card>
  );
}

/** Persona hosted flow in an auth session that returns to the app. */
function StartButton({ label, status }: { label: string; status: IdentityStatus }) {
  const start = useStartVerification();
  const statusQuery = useIdentityStatus();
  const run = () => {
    const redirect = Linking.createURL("/settings/identity");
    start.mutate(
      { redirect_uri: redirect },
      {
        onSuccess: async (res) => {
          if (res.flow?.hosted_url) {
            await WebBrowser.openAuthSessionAsync(res.flow.hosted_url, redirect);
            void statusQuery.refetch();
          }
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't start verification. Try again.")),
      },
    );
  };
  return (
    <Button leftIcon={ShieldCheck} loading={start.isPending} disabled={status.status === "rejected" && status.attempts_left === 0} onPress={run}>
      {label}
    </Button>
  );
}

function DocumentsForm() {
  const theme = useTheme();
  const s = theme.space;
  const upload = useUpload();
  const submit = useSubmitIdentityDocuments();
  const [doc, setDoc] = useState<UploadedPhoto[]>([]);
  const [selfie, setSelfie] = useState<UploadedPhoto[]>([]);
  const [busy, setBusy] = useState(0);
  const docKey = doc[0]?.key;
  const selfieKey = selfie[0]?.key;
  const up = async (file: Parameters<typeof upload.mutateAsync>[0]["file"], signal: AbortSignal) => {
    const r = await upload.mutateAsync({ file, purpose: "identity_document", signal });
    return { key: r.key, url: null };
  };
  const onBusy = (u: boolean) => setBusy((n) => n + (u ? 1 : -1));
  return (
    <View style={{ gap: s[6] }}>
      <Field label="Photo of your ID" info="A passport, driving licence or national ID card. All four corners in frame, text readable.">
        <PhotoUploader max={1} quality={0.9} onChange={setDoc} onUploadingChange={onBusy} upload={(f, { signal }) => up(f, signal)} />
      </Field>
      <Field label="Selfie" info="Your face, well lit, no sunglasses or hat.">
        <PhotoUploader max={1} quality={0.9} sources={["camera"]} onChange={setSelfie} onUploadingChange={onBusy} upload={(f, { signal }) => up(f, signal)} />
      </Field>
      <Button
        disabled={!docKey || !selfieKey || busy > 0}
        loading={submit.isPending}
        onPress={() =>
          submit.mutate(
            { document_key: docKey ?? "", selfie_key: selfieKey ?? "" },
            {
              onSuccess: () => {
                haptic.success();
                toast.success("Sent for review");
              },
              onError: (e) => toast.error(errorMessage(e, "Couldn't send your photos. Try again.")),
            },
          )
        }
      >
        Send for review
      </Button>
    </View>
  );
}

function Body_({ status }: { status: IdentityStatus }) {
  const theme = useTheme();
  const c = theme.colors;
  const config = useConfig();
  const icon = (I: typeof Clock, color = c.text) => <I size={22} color={color} strokeWidth={2} />;
  const manual = status.provider === "manual";
  switch (status.status) {
    case "verified":
      return (
        <StateCard
          icon={icon(BadgeCheck, c.primaryStrong)}
          title="Verified"
          info={[status.verified_at ? `Verified ${dateText(status.verified_at)}.` : "", status.expires_at ? `Valid until ${dateText(status.expires_at)}.` : ""].filter(Boolean).join(" ") || undefined}
        />
      );
    case "pending":
      if (status.submitted) return <StateCard icon={icon(Clock)} title="In review" info="You'll get a notification when it's done." />;
      return manual ? (
        <>
          <StateCard icon={icon(ShieldQuestion)} title="Not verified yet" info="Staff look at your photos once to confirm your identity, then delete them." />
          <DocumentsForm />
        </>
      ) : (
        <StateCard icon={icon(ShieldQuestion)} title="Verification started">
          <StartButton label="Continue verification" status={status} />
        </StateCard>
      );
    case "rejected":
      return (
        <StateCard icon={icon(ShieldAlert, c.dangerStrong)} title="Not verified">
          {status.reason ? <Banner tone="danger" title={status.reason} /> : null}
          {status.attempts_left > 0 ? (
            <StartButton label="Try again" status={status} />
          ) : config.data?.support_email ? (
            <Button variant="secondary" onPress={() => void Linking.openURL(`mailto:${config.data?.support_email}?subject=Identity%20verification`)}>
              Contact support
            </Button>
          ) : null}
        </StateCard>
      );
    case "expired":
      return (
        <StateCard icon={icon(ShieldQuestion)} title="Verification expired">
          <StartButton label="Verify again" status={status} />
        </StateCard>
      );
    default:
      return (
        <StateCard icon={icon(ShieldQuestion)} title="Not verified" info={manual ? "You send a photo of your ID and a selfie; staff confirm it." : "A verification partner checks your ID and a selfie."}>
          <StartButton label="Verify your identity" status={status} />
        </StateCard>
      );
  }
}

/** Settings → Verification: status and the next step. */
export function IdentityScreen() {
  const theme = useTheme();
  const s = theme.space;
  const status = useIdentityStatus();
  return (
    <Screen edges={["bottom"]} gap={s[6]}>
      {status.isPending ? (
        <View accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton height={96} />
        </View>
      ) : status.isError ? (
        <ErrorState title="Couldn't load your verification" info={errorMessage(status.error)} onRetry={() => void status.refetch()} retrying={status.isRefetching} />
      ) : (
        <Body_ status={status.data} />
      )}
    </Screen>
  );
}
