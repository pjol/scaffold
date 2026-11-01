/**
 * Organizations UI for stitching:
 *
 *   import { OrgsScreen, JoinableOrgs } from "@/features/orgs";
 *   const { org } = useActiveOrg(); // from @scaffoldapp/shared
 *
 * JoinableOrgs lists orgs the person's verified email domain lets them join; put it on an
 * onboarding screen for people without an org.
 */
export { OrgsScreen } from "./OrgsScreen";
export { OrgScreen } from "./OrgScreen";
export { InviteScreen } from "./InviteScreen";
export { JoinableOrgs } from "./JoinableOrgs";
