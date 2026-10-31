"use client";

import { SupportContact } from "@/components/account/SupportContact";
import { SupportLinks } from "@/components/account/SupportLinks";
import { Screen } from "@/components/shell/PageHeader";

/** Settings → Support: the published contact (Apple 1.2 / 5.1.1) and the registry's `supportLinks`. */
export function SupportScreen() {
  return (
    <Screen title="Support" info="Questions, problems with your account, or anything else.">
      <div className="flex flex-col gap-4">
        <SupportContact />
        <SupportLinks variant="list" />
      </div>
    </Screen>
  );
}
