import { p, section, ul } from "./blocks";
import type { LegalDocument } from "./types";
import { TERMS_VERSION } from "./versions";

/**
 * Terms of Service: a plain-English starting template, not legal advice. Fill every
 * placeholder (`LEGAL_PLACEHOLDERS`), remove what the product doesn't do, add what it
 * does (payments, marketplaces, user-to-user content), and have counsel review it before
 * launch. Changing the text means bumping `TERMS_VERSION` here and in the backend.
 */
export const TERMS_OF_SERVICE: LegalDocument = {
  kind: "terms",
  version: TERMS_VERSION,
  title: "Terms of Service",
  effective: "2026-01-01",
  sections: [
    section(
      "about",
      "About these Terms",
      p(
        "These Terms of Service (the “Terms”) are an agreement between you and [Company legal name] (“we,” “us,” or “our”), located at [Company address]. They govern your use of Scaffold App, including our websites, mobile apps and related services (together, the “Services”).",
      ),
      p(
        "By creating an account, accepting these Terms in the app, or using the Services, you agree to these Terms and to our Privacy Policy, which explains how we handle personal information. If you do not agree, do not use the Services.",
      ),
    ),
    section(
      "eligibility",
      "Who can use the Services",
      ul(
        "You must be at least 13 years old, or the minimum age required to use online services without parental consent where you live, if that is higher.",
        "If you use the Services for an organization, you confirm you are authorized to accept these Terms for it, and the organization is bound by them.",
        "You may not use the Services if we have previously suspended or banned your account, or if the law prohibits you from using them.",
      ),
    ),
    section(
      "accounts",
      "Your account",
      p("You sign in through our authentication provider. You agree to:"),
      ul(
        "give accurate information and keep it up to date;",
        "keep your sign-in credentials secure and not share your account;",
        "tell us at [Contact email] if you believe someone else has accessed your account.",
      ),
      p("You are responsible for activity under your account unless it results from our failure to protect it."),
    ),
    section(
      "your-content",
      "Your content",
      p(
        "You keep ownership of the text, photos, files and other material you submit (“your content”). You give us a worldwide, non-exclusive, royalty-free licence to host, store, copy, process and display your content only as needed to operate, secure and improve the Services for you. This licence ends when your content is deleted, except for copies we must keep by law or that remain in backups for a limited time.",
      ),
      p("You confirm you have the rights to the content you submit and that it does not break these Terms or the law."),
    ),
    section(
      "acceptable-use",
      "Acceptable use",
      p("You agree not to:"),
      ul(
        "break the law or help anyone else do so;",
        "harass, threaten, impersonate or defraud anyone;",
        "upload content that is illegal, infringes someone else's rights, or contains malware;",
        "access accounts or data that are not yours, or probe, scan or test the security of the Services without our written permission;",
        "overload, disrupt or reverse engineer the Services, or access them by automated means except through interfaces we provide for that purpose;",
        "resell or commercially exploit the Services without our written permission.",
      ),
    ),
    section(
      "payments",
      "Paid features",
      p(
        "If the Services offer paid features, the price, billing period and renewal terms are shown before you pay. Payments are handled by our payment processor; we do not store full card numbers. Unless the law or the offer says otherwise, fees already paid are not refundable.",
      ),
      p("We keep records of transactions for as long as tax and accounting law requires, including after you delete your account."),
    ),
    section(
      "third-parties",
      "Third-party services",
      p(
        "The Services rely on third-party providers (for example, for sign-in, file storage and email) and may link to services we do not control. Their own terms and privacy policies apply to your use of them, and we are not responsible for them.",
      ),
    ),
    section(
      "deleting-your-account",
      "Suspension, termination and deleting your account",
      p(
        "You can delete your account at any time from the app's settings. Deletion is permanent: we remove your profile, sign-in account and uploaded files, and delete or anonymize the rest of your personal information as described in the Privacy Policy. Before deleting, you can download a copy of your data from the same screen.",
      ),
      p(
        "We may suspend or close your account if you break these Terms, if the law requires it, or to protect other users or the Services. Where reasonable, we will tell you why and give you a chance to respond. A suspended account can still sign in to read its data and to delete itself.",
      ),
    ),
    section(
      "changes",
      "Changes to the Services and these Terms",
      p(
        "We may change the Services over time. We may also update these Terms. Each version has a date, shown at the top. When we make a change that affects your rights, we will ask you to review and accept the new version in the app before you continue. If you do not accept, you can stop using the Services and delete your account.",
      ),
    ),
    section(
      "disclaimers",
      "Disclaimers",
      p(
        "We work to keep the Services available and correct, but they are provided “as is” and “as available.” To the extent the law allows, we disclaim all warranties, express or implied, including merchantability, fitness for a particular purpose and non-infringement. Nothing in these Terms limits rights you have under consumer protection laws that cannot be waived.",
      ),
    ),
    section(
      "liability",
      "Limitation of liability",
      p(
        "To the extent the law allows, we are not liable for indirect, incidental, special, consequential or punitive damages, or for lost profits, data or goodwill. Our total liability for any claim relating to the Services is limited to the greater of the amount you paid us in the 12 months before the claim and 100 US dollars (or the equivalent in your currency). These limits do not apply to liability that cannot be limited by law, such as for fraud, or for death or personal injury caused by negligence.",
      ),
    ),
    section(
      "law",
      "Governing law and disputes",
      p(
        "These Terms are governed by the laws of [Governing jurisdiction], without regard to its conflict-of-law rules. Disputes will be resolved in the courts of [Governing jurisdiction], unless the law where you live gives you the right to bring a claim in your local courts. Before filing a claim, please contact us at [Contact email] so we can try to resolve it informally.",
      ),
    ),
    section(
      "general",
      "General",
      ul(
        "If a provision of these Terms is found unenforceable, the rest remain in effect.",
        "Not enforcing a provision is not a waiver of our right to enforce it later.",
        "You may not transfer these Terms without our consent. We may transfer them as part of a merger, acquisition or sale of assets, and will tell you if we do.",
        "These Terms and the Privacy Policy are the entire agreement between you and us about the Services.",
      ),
    ),
    section("contact", "Contact", p("[Company legal name], [Company address]. Email: [Contact email].")),
  ],
};
