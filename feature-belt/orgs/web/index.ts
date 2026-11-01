/**
 * Organizations UI other features and stitching import:
 *
 *   import { OrgSwitcher, CreateOrgDialog, JoinableOrgs } from "@/features/orgs";
 *   const { org } = useActiveOrg(); // from @scaffoldapp/shared
 *
 * JoinableOrgs lists orgs the person's verified email domain lets them join; put it on an
 * onboarding screen for people without an org.
 */
export { OrgSwitcher } from "./OrgSwitcher";
export { OrgMenuItems } from "./OrgMenuItems";
export { CreateOrgDialog } from "./CreateOrgDialog";
export { JoinableOrgs } from "./JoinableOrgs";
