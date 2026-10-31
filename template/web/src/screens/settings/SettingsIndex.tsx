"use client";

import { ProfileScreen } from "./ProfileScreen";
import { SettingsList } from "./SettingsList";

/** `/settings`: the section list on phones; Profile (the first tab) from 1024 px. */
export function SettingsIndex() {
  return (
    <>
      <div className="lg:hidden">
        <SettingsList />
      </div>
      <div className="hidden lg:block">
        <ProfileScreen />
      </div>
    </>
  );
}
