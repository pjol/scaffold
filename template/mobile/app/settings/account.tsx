import { useUser } from "@clerk/expo";
import { errorMessage, useAuthSession, useDeleteAccount, useExportAccount } from "@scaffoldapp/shared";
import { Body, ConfirmDialog, Field, Input, ListGroup, ListRow, Screen, Spinner, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import { File, Paths } from "expo-file-system";
import * as Sharing from "expo-sharing";
import { Download, KeyRound, Trash2 } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { useSignOut } from "@/lib/session";
import { appName } from "@/lib/version";

const CONFIRM_WORD = "DELETE";

/** Download my data, passkeys (Clerk) and Delete account: Settings → Account → Delete account → confirm. */
export default function AccountScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const session = useAuthSession();
  const exportAccount = useExportAccount();
  const [confirm, setConfirm] = useState(false);

  const download = async () => {
    try {
      const data = await exportAccount.mutateAsync();
      const file = new File(Paths.cache, `${appName().toLowerCase().replace(/[^a-z0-9]+/g, "-")}-data-${data.exported_at.slice(0, 10)}.json`);
      if (file.exists) file.delete();
      file.create();
      file.write(JSON.stringify(data, null, 2));
      if (!(await Sharing.isAvailableAsync())) {
        toast.error("Sharing isn't available on this device.");
        return;
      }
      await Sharing.shareAsync(file.uri, { mimeType: "application/json", UTI: "public.json", dialogTitle: `Your ${appName()} data` });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't prepare your data. Try again."));
    }
  };

  return (
    <Screen edges={["bottom"]} gap={theme.space[6]}>
      <ListGroup title="Your data">
        <ListRow
          title="Download my data"
          info="A JSON file with your profile, agreements and everything the app stores about you, shared to wherever you choose."
          leading={<Download size={22} color={c.textMuted} strokeWidth={2} />}
          trailing={exportAccount.isPending ? <Spinner size={20} label="Preparing" /> : undefined}
          chevron={false}
          disabled={exportAccount.isPending}
          onPress={() => void download()}
        />
      </ListGroup>
      {session.mode === "clerk" ? <PasskeyGroup /> : null}
      <ListGroup>
        <ListRow title="Delete account" destructive leading={<Trash2 size={22} color={c.dangerStrong} strokeWidth={2} />} chevron={false} onPress={() => setConfirm(true)} />
      </ListGroup>
      <DeleteAccountDialog open={confirm} onClose={() => setConfirm(false)} />
    </Screen>
  );
}

/** Add a passkey to the Clerk account (sign in later with Face ID / fingerprint). */
function PasskeyGroup() {
  const theme = useTheme();
  const { user } = useUser();
  const [busy, setBusy] = useState(false);
  const count = user?.passkeys?.length ?? 0;
  return (
    <ListGroup title="Sign-in" info="Passkeys sign you in with Face ID, Touch ID or your screen lock instead of an email code.">
      <ListRow
        title={count > 0 ? `Add another passkey (${count} saved)` : "Add a passkey"}
        leading={<KeyRound size={22} color={theme.colors.textMuted} strokeWidth={2} />}
        trailing={busy ? <Spinner size={20} label="Creating passkey" /> : undefined}
        chevron={false}
        disabled={!user || busy}
        onPress={async () => {
          if (!user) return;
          setBusy(true);
          try {
            await user.createPasskey();
            haptic.success();
            toast.success("Passkey added");
          } catch (e) {
            const err = e as { errors?: { code?: string; longMessage?: string; message?: string }[] };
            const first = err.errors?.[0];
            if (first?.code !== "passkey_registration_cancelled") toast.error(first?.longMessage ?? first?.message ?? "Couldn't add a passkey. Try again.");
          } finally {
            setBusy(false);
          }
        }}
      />
    </ListGroup>
  );
}

/** States what goes and what stays, asks for DELETE, deletes, then signs out and wipes local data. */
function DeleteAccountDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const theme = useTheme();
  const del = useDeleteAccount();
  const signOut = useSignOut();
  const [typed, setTyped] = useState("");
  return (
    <ConfirmDialog
      open={open}
      onClose={() => {
        setTyped("");
        onClose();
      }}
      title="Delete your account?"
      tone="danger"
      confirmLabel="Delete account"
      confirmDisabled={typed.trim().toUpperCase() !== CONFIRM_WORD}
      errorText={(e) => errorMessage(e, "Couldn't delete your account. Try again.")}
      onConfirm={async () => {
        await del.mutateAsync();
        haptic.success();
        await signOut();
        toast.success("Your account is deleted.");
      }}
    >
      <View style={{ gap: theme.space[3] }}>
        <Body>This deletes your profile, photo, sign-in and everything you created, right away. It can't be undone.</Body>
        <Body tone="muted">Records the law requires us to keep, such as payments, stay for up to 7 years without your name.</Body>
        <Field label={`Type ${CONFIRM_WORD} to confirm`}>
          <Input value={typed} onChangeText={setTyped} autoCapitalize="characters" autoCorrect={false} autoComplete="off" placeholder={CONFIRM_WORD} accessibilityLabel={`Type ${CONFIRM_WORD} to confirm`} />
        </Field>
      </View>
    </ConfirmDialog>
  );
}
