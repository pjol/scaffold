import { p, section, ul } from "./blocks";
import type { LegalDocument } from "./types";
import { PRIVACY_VERSION } from "./versions";

/**
 * Privacy Policy: a plain-English starting template, not legal advice. It describes what
 * the core template actually does (sign-in, profile, uploads, consent records, export,
 * deletion). Every feature that collects more (location, payments, push tokens,
 * messages, analytics) must add itself to "Information we collect" and "Service
 * providers". Fill the placeholders and have counsel review it before launch. Changing
 * the text means bumping `PRIVACY_VERSION` here and in the backend.
 */
export const PRIVACY_POLICY: LegalDocument = {
  kind: "privacy",
  version: PRIVACY_VERSION,
  title: "Privacy Policy",
  effective: "2026-01-01",
  sections: [
    section(
      "who-we-are",
      "Who we are",
      p(
        "This policy explains how [Company legal name] (“we,” “us,” or “our”), located at [Company address], collects, uses and shares personal information when you use Scaffold App (the “Services”). We are the controller of that information. Questions and requests go to [Contact email].",
      ),
    ),
    section(
      "information-we-collect",
      "Information we collect",
      p("Information you give us or that our sign-in provider shares with us:"),
      ul(
        "Account information: your email address, name and profile photo.",
        "Content you submit: text, photos and files you upload or create in the Services.",
        "Messages you send us, such as support requests.",
      ),
      p("Information collected automatically:"),
      ul(
        "Consent records: which versions of the Terms and this policy you accepted, when, on which platform, and from which IP address.",
        "Technical information: IP address, device platform (web, iOS or Android), app version, and request logs we need to run and secure the Services.",
      ),
      p("We do not use third-party advertising trackers in the Services."),
    ),
    section(
      "how-we-use-it",
      "How we use information",
      ul(
        "To provide the Services: create and run your account, store your content, and show it to you and to the people you choose to share it with.",
        "To keep the Services safe: prevent fraud and abuse, enforce our Terms, and investigate security incidents.",
        "To communicate with you: service messages such as security alerts, changes to our terms, and replies to your requests.",
        "To improve the Services: understand which features are used and fix problems.",
        "To meet legal obligations: keep records the law requires and respond to lawful requests.",
      ),
    ),
    section(
      "legal-bases",
      "Legal bases (EEA and UK)",
      p(
        "If you are in the European Economic Area or the United Kingdom, we process your information because it is necessary to perform our contract with you (providing the Services), for our legitimate interests (security, abuse prevention and improving the Services, balanced against your rights), to comply with legal obligations, or with your consent, which you can withdraw at any time.",
      ),
    ),
    section(
      "sharing",
      "How we share information",
      p("We do not sell your personal information, and we do not share it for cross-context behavioral advertising. We share it only:"),
      ul(
        "with service providers that process it for us under contract (listed below);",
        "with other users, when you choose to share content with them;",
        "when the law requires it, or to protect the rights, safety and property of our users, the public or us;",
        "as part of a merger, acquisition or sale of assets, in which case we will tell you before your information becomes subject to a different policy.",
      ),
    ),
    section(
      "service-providers",
      "Service providers",
      p("These companies process personal information on our behalf, only to provide their service to us:"),
      ul(
        "Clerk: sign-in and account security.",
        "Amazon Web Services: storage of uploaded files.",
        "Resend: delivery of email.",
        "[Hosting provider]: application servers and database.",
      ),
      p("We update this list when it changes."),
    ),
    section(
      "retention",
      "How long we keep information",
      ul(
        "Account information and content: while your account exists.",
        "When you delete your account: we immediately anonymize your profile, and remove your sign-in account and uploaded files, normally within minutes and always within 30 days. Content that other users received from you may remain visible to them in anonymized form.",
        "Consent records and records of transactions: for as long as the law requires us to keep them, which can be several years after you delete your account.",
        "Request logs and backups: for a limited period, after which they are deleted or overwritten.",
      ),
    ),
    section(
      "your-rights",
      "Your choices and rights",
      ul(
        "Access and export: download a copy of your data as a JSON file from the app's settings.",
        "Correction: edit your name and photo in the app's settings.",
        "Deletion: delete your account and personal information from the app's settings.",
        "Objection and restriction: ask us to stop or limit certain processing by emailing [Contact email].",
      ),
      p(
        "Depending on where you live (including the EEA, the UK and California), you may have further rights, such as to know what we collect and why, and not to be treated differently for using your rights. Email [Contact email] to make a request; we will verify that the request comes from you and respond within the time the law allows. You can also complain to your local data protection authority.",
      ),
    ),
    section(
      "security",
      "Security",
      p(
        "We protect information with encryption in transit, access controls and monitoring. No system is completely secure; if a breach affects your personal information, we will notify you and the authorities as the law requires.",
      ),
    ),
    section(
      "transfers",
      "International transfers",
      p(
        "We and our service providers may process information in countries other than yours, including the United States. Where the law requires it, we protect those transfers with safeguards such as the European Commission's Standard Contractual Clauses.",
      ),
    ),
    section(
      "children",
      "Children",
      p(
        "The Services are not directed to children under 13, or under the minimum age for online services without parental consent where they live. We do not knowingly collect their personal information. If you believe a child has given us personal information, contact [Contact email] and we will delete it.",
      ),
    ),
    section(
      "changes",
      "Changes to this policy",
      p(
        "Each version of this policy has a date, shown at the top. When we make a change that affects your rights, we will ask you to review and accept the new version in the app before you continue.",
      ),
    ),
    section("contact", "Contact", p("[Company legal name], [Company address]. Email: [Contact email].")),
  ],
};
