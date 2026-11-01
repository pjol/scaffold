package auth

// DevIdentity is a development account. packages/shared/src/dev.ts lists the
// same identities for the web and mobile dev sign-in; keep them in step.
type DevIdentity struct {
	Subject     string // stored as auth_subject
	Email       string // .test is reserved: dev mail can never reach a real inbox
	DisplayName string
	AdminRole   string
}

// DevIdentities are created by cmd/seed. The first is the admin; dev-up puts
// its email in ADMIN_EMAILS.
var DevIdentities = []DevIdentity{
	{Subject: "dev_admin", Email: "admin@scaffoldapp.test", DisplayName: "Avery Admin", AdminRole: "admin"},
	{Subject: "dev_sam", Email: "sam@scaffoldapp.test", DisplayName: "Sam Rivera"},
	{Subject: "dev_jordan", Email: "jordan@scaffoldapp.test", DisplayName: "Jordan Lee"},
	{Subject: "dev_priya", Email: "priya@scaffoldapp.test", DisplayName: "Priya Shah"},
}
