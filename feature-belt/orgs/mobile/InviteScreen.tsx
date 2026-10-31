import { errorMessage, setActiveOrgId, useAcceptInvite, useAuthSession, useInvitePreview, type InvitePreviewStatus } from "@scaffoldapp/shared";
import { Avatar, Banner, Button, EmptyState, ErrorState, haptic, InfoTip, Screen, Skeleton, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Link2, LogIn } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { rememberRoute } from "@/lib/pending-route";

const UNAVAILABLE: Record<Exclude<InvitePreviewStatus, "valid">, string> = {
  expired: "This invite expired",
  revoked: "This invite was withdrawn",
  used: "This link has reached its limit",
  accepted: "This invite was already used",
  declined: "This invite was declined",
};

/**
 * `<scheme>://invites/<token>`: an org invite (email or link). A public route: signed out,
 * it previews the invite and "Sign in to join" comes back here after sign-in and consent.
 */
export function InviteScreen() {
  const theme = useTheme();
  const s = theme.space;
  const { token } = useLocalSearchParams<{ token: string }>();
  const session = useAuthSession();
  const signedIn = session.isLoaded && session.isSignedIn;
  const preview = useInvitePreview(token);
  const accept = useAcceptInvite();
  const [problem, setProblem] = useState<string | null>(null);
  const p = preview.data;

  const join = () => {
    if (!token) return;
    setProblem(null);
    accept.mutate(token, {
      onSuccess: (org) => {
        haptic.success();
        setActiveOrgId(org.id);
        toast.success(`You joined ${org.name}`);
        router.replace({ pathname: "/settings/orgs/[orgID]", params: { orgID: org.id } });
      },
      onError: (e) => setProblem(errorMessage(e, "Couldn't join. Try again.")),
    });
  };

  return (
    <Screen
      edges={["bottom"]}
      gap={s[6]}
      centered
      footer={
        p?.status === "valid" && !signedIn ? (
          <Button
            fullWidth
            leftIcon={LogIn}
            onPress={() => {
              rememberRoute(`/invites/${token ?? ""}`);
              router.push("/sign-in");
            }}
          >
            Sign in to join
          </Button>
        ) : p?.status === "valid" ? (
          <View style={{ gap: s[2] }}>
            <Button fullWidth loading={accept.isPending} onPress={join}>
              Join
            </Button>
            <Button fullWidth variant="ghost" onPress={() => (router.canGoBack() ? router.back() : router.replace("/"))}>
              Not now
            </Button>
          </View>
        ) : undefined
      }
    >
      <Stack.Screen options={{ title: "Invitation" }} />
      {preview.isPending ? (
        <View style={{ alignItems: "center", gap: s[4] }} accessibilityLabel="Loading" accessibilityRole="progressbar">
          <Skeleton shape="circle" width={96} height={96} />
          <Skeleton shape="text" width={220} height={28} />
        </View>
      ) : preview.isError ? (
        preview.error.status === 404 ? (
          <EmptyState icon={Link2} title="This invite link isn't valid" info="Check the link, or ask for a new one." />
        ) : (
          <ErrorState title="Couldn't load the invite" info={errorMessage(preview.error)} onRetry={() => void preview.refetch()} retrying={preview.isRefetching} />
        )
      ) : p && p.status !== "valid" ? (
        <EmptyState icon={Link2} title={UNAVAILABLE[p.status]} info="Ask whoever invited you for a new link." />
      ) : p ? (
        <View style={{ alignItems: "center", gap: s[4] }}>
          <Avatar name={p.org.name} src={p.org.avatar_url} size="2xl" />
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Title align="center">{p.org.name}</Title>
            <InfoTip
              content={`${p.invited_by_name ? `${p.invited_by_name} invited you` : "You're invited"} as ${p.role === "admin" ? "an admin" : "a member"}.${p.email_hint ? ` The invite is for ${p.email_hint}.` : ""}`}
            />
          </View>
          {problem ? <Banner tone="danger" title={problem} /> : null}
        </View>
      ) : null}
    </Screen>
  );
}
