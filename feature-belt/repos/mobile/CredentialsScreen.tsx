import { errorMessage, reposRelativeTime, useCreateReposCredential, useReposCredentials, useRevokeReposCredential, type ReposCreatedCredential } from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, Field, Input, ListGroup, ListRow, Screen, SegmentedControl, Sheet, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack } from "expo-router";
import { KeyRound, Plus } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, Share, View } from "react-native";
import { MONO } from "./parts";

/** Settings → Git credentials: passwords for git over HTTPS; revoke the ones you no longer use. */
export function CredentialsScreen() {
  const theme = useTheme();
  const creds = useReposCredentials();
  const revoke = useRevokeReposCredential();
  const [creating, setCreating] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const rows = creds.data?.items ?? [];
  return (
    <Screen
      edges={["bottom"]}
      refreshControl={<RefreshControl refreshing={creds.isRefetching} onRefresh={() => void creds.refetch()} />}
      footer={
        <Button fullWidth leftIcon={Plus} onPress={() => setCreating(true)}>
          New credential
        </Button>
      }
    >
      <Stack.Screen options={{ title: "Git credentials" }} />
      <ListGroup>
        {creds.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
          </View>
        ) : creds.isError ? (
          <ErrorState compact title="Couldn't load credentials" info={errorMessage(creds.error)} onRetry={() => void creds.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={KeyRound} title="No git credentials" info="Use one as the password when git asks. They work only for git, never for signing in or AI agents." />
        ) : (
          rows.map((c) => (
            <ListRow
              key={c.id}
              title={c.name}
              info={`${c.prefix}… · ${c.last_used_at ? `used ${reposRelativeTime(c.last_used_at)}` : "never used"} · ${c.expires_at ? `expires ${new Date(c.expires_at).toLocaleDateString()}` : "no expiry"}`}
              trailing={<Badge tone={c.access === "write" ? "warning" : "neutral"}>{c.access === "write" ? "Read and write" : "Read"}</Badge>}
              onPress={() => setRevoking(c.id)}
              chevron={false}
              accessibilityHint="Revokes it"
            />
          ))
        )}
      </ListGroup>
      <CreateSheet open={creating} onClose={() => setCreating(false)} />
      <ConfirmDialog
        open={!!revoking}
        onClose={() => setRevoking(null)}
        title="Revoke this credential?"
        tone="danger"
        confirmLabel="Revoke"
        onConfirm={async () => {
          if (revoking) await revoke.mutateAsync(revoking);
          setRevoking(null);
        }}
      >
        <Text>Git on any computer using it stops working at once.</Text>
      </ConfirmDialog>
      <View style={{ height: theme.space[2] }} />
    </Screen>
  );
}

function CreateSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const create = useCreateReposCredential();
  const [name, setName] = useState("");
  const [access, setAccess] = useState<"read" | "write">("write");
  const [made, setMade] = useState<ReposCreatedCredential | null>(null);
  const close = () => {
    setMade(null);
    setName("");
    onClose();
  };
  return (
    <Sheet open={open} onClose={close} title={made ? "Copy your credential now" : "New git credential"} snapPoints={["70%"]}>
      {made ? (
        <View style={{ gap: theme.space[4] }}>
          <Text tone="muted">It's shown only now. Save it in your password manager.</Text>
          <Text selectable role="label" style={{ fontFamily: MONO, backgroundColor: theme.colors.sunken, padding: theme.space[3], borderRadius: theme.radius.md }}>
            {made.secret}
          </Text>
          <Button variant="secondary" onPress={() => void Share.share({ message: made.secret })}>
            Share to a password manager
          </Button>
          <Button onPress={close}>Done</Button>
        </View>
      ) : (
        <View style={{ gap: theme.space[4] }}>
          <Field label="Name">
            <Input value={name} onChangeText={setName} placeholder="Work laptop" maxLength={60} autoFocus />
          </Field>
          <SegmentedControl<"read" | "write"> accessibilityLabel="Access" value={access} onValueChange={setAccess} options={[{ value: "read", label: "Read" }, { value: "write", label: "Read and write" }]} />
          <Button
            fullWidth
            disabled={!name.trim()}
            loading={create.isPending}
            onPress={() =>
              create.mutate(
                { name: name.trim(), access, expires_in_days: 90 },
                { onSuccess: setMade, onError: (e) => toast.error(errorMessage(e, "Couldn't make the credential.")) },
              )
            }
          >
            Make credential (90 days)
          </Button>
        </View>
      )}
    </Sheet>
  );
}
