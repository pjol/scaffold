"use client";

import { errorMessage, useExportAccount } from "@scaffoldapp/shared";
import { Card, Divider, ListRow, Spinner, toast } from "@scaffoldapp/ui-web";
import { Download, Trash2 } from "lucide-react";
import { useState } from "react";
import { DeleteAccountDialog } from "@/components/account/DeleteAccountDialog";
import { useSignOut } from "@/components/auth/useSignOut";
import { Screen } from "@/components/shell/PageHeader";
import { downloadJson } from "@/lib/download";

const tile = "flex size-8 items-center justify-center rounded-md";

/**
 * Settings → Account: download my data (GDPR/CCPA export, saved as JSON) and delete
 * account. Delete is 3 taps from Settings: Account → Delete account → confirm.
 */
export function AccountScreen() {
  const exportAccount = useExportAccount();
  const signOut = useSignOut();
  const [deleting, setDeleting] = useState(false);

  const download = () => {
    if (exportAccount.isPending) return;
    exportAccount.mutate(undefined, {
      onSuccess: (data) => {
        downloadJson(data, "account-export.json");
        toast.success("Saved account-export.json");
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't prepare your data. Try again.")),
    });
  };

  return (
    <Screen title="Account">
      <Card padding="none" className="p-1">
        <ListRow
          title="Download my data"
          onClick={download}
          aria-busy={exportAccount.isPending || undefined}
          chevron={false}
          leading={
            <span className={`${tile} bg-sunken text-fg`}>
              <Download className="size-4" aria-hidden />
            </span>
          }
          trailing={exportAccount.isPending ? <Spinner size={16} label="Preparing your data" /> : null}
        />
        <Divider className="mx-4" />
        <ListRow
          title="Delete account"
          destructive
          onClick={() => setDeleting(true)}
          chevron={false}
          leading={
            <span className={`${tile} bg-danger-soft text-danger-strong`}>
              <Trash2 className="size-4" aria-hidden />
            </span>
          }
        />
      </Card>
      <DeleteAccountDialog
        open={deleting}
        onOpenChange={setDeleting}
        onDeleted={async () => {
          toast.success("Your account was deleted.");
          await signOut("/sign-in?deleted=1");
        }}
      />
    </Screen>
  );
}
