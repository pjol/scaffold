package files

import (
	"net/http"
	"testing"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

func TestExternalTarget(t *testing.T) {
	conn := "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e"
	g := &graphDrive{}
	if got := externalTarget(conn, g.externalID("b!Xy-z~01ABC!12")); got != conn+":b.Xy-z/01ABC.12" {
		t.Fatalf("graph id: %q", got)
	}
	if got := externalTarget(conn, "1AbC_d-9"); got != conn+":1AbC_d-9" {
		t.Fatalf("drive id: %q", got)
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	if got := externalTarget(conn, string(long)); len(got) != 200 {
		t.Fatalf("capped at 200: %d", len(got))
	}
}

func TestGraphRefs(t *testing.T) {
	g := &graphDrive{base: "https://graph.microsoft.com/v1.0"}
	for ref, want := range map[string]string{
		"b!abc~root":   "/drives/b%21abc/root",
		"b!abc~01XYZ":  "/drives/b%21abc/items/01XYZ",
		"D46!1~D46!54": "/drives/D46%211/items/D46%2154",
	} {
		got, err := g.itemPath(ref)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", ref, got, err)
		}
	}
	if _, err := g.itemPath("nodrive"); err == nil {
		t.Fatal("a ref without a drive")
	}
	if g.sameHost("https://graph.microsoft.com/v1.0/drives/x/root/children?$skiptoken=2") == "" || g.sameHost("https://evil.example/v1.0/x") != "" {
		t.Fatal("nextLink host check")
	}
	if !(&graphDrive{base: "https://graph.microsoft.com/v1.0"}).allowedDownload("https://acme.sharepoint.com/dl?x") ||
		(&graphDrive{base: "https://graph.microsoft.com/v1.0"}).allowedDownload("http://acme.sharepoint.com/dl") {
		t.Fatal("download links must be https")
	}
}

func TestGoogleQuery(t *testing.T) {
	if got := googleOrder("updated", true); got != "folder,modifiedTime desc" {
		t.Fatal(got)
	}
	if got := googleOrder("", false); got != "folder,name_natural" {
		t.Fatal(got)
	}
	if got := driveQuote(`it's a \ test`); got != `it\'s a \\ test` {
		t.Fatal(got)
	}
	g := newGoogleDrive(&connInfo{Connection: app.Connection{Endpoints: map[string]string{"api": "https://www.googleapis.com"}}}, http.DefaultClient, nil)
	if g.base != "https://www.googleapis.com/drive/v3" || g.upload != "https://www.googleapis.com/upload/drive/v3" {
		t.Fatalf("endpoints: %s %s", g.base, g.upload)
	}
}

func TestPlatformErrors(t *testing.T) {
	p := platform{name: "OneDrive", wrap: func(err error) error { return err }}
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{}`, app.ConnectionNeedsAuth},
		{403, `{"error":{"code":403,"message":"x","errors":[{"reason":"userRateLimitExceeded"}]}}`, app.ConnectionRateLimited},
		{403, `{"error":{"code":"accessDenied","message":"no"}}`, "connected_forbidden"},
		{404, `{"error":{"code":"itemNotFound"}}`, "not_found"},
		{409, `{"error":{"code":"nameAlreadyExists","message":"Name already exists"}}`, "name_taken"},
		{412, `{"error":{"code":"resourceModified"}}`, "target_changed"},
		{429, `{}`, app.ConnectionRateLimited},
		{503, `{}`, app.ConnectionUnavailable},
	}
	for _, c := range cases {
		err := p.statusError(&http.Response{StatusCode: c.status, Header: http.Header{"Retry-After": {"3"}}}, []byte(c.body))
		he, ok := err.(*httpx.Error)
		if !ok || he.Code != c.want || he.Status == http.StatusUnauthorized {
			t.Fatalf("%d: %v", c.status, err)
		}
	}
}

func TestConnectedPreview(t *testing.T) {
	gd := &googleDrive{}
	ms := &graphDrive{}
	docx := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	for _, c := range []struct {
		ad   driveAdapter
		it   extItem
		want string
	}{
		{gd, extItem{Native: "Google Docs", NativeExport: docx}, "pdf"},
		{gd, extItem{Native: "Google Forms"}, "other"},
		{gd, extItem{Name: "a.docx", MimeType: docx}, "office"},
		{ms, extItem{Name: "a.docx", MimeType: docx}, "pdf"},
		{ms, extItem{Name: "a.png", MimeType: "image/png"}, "image"},
		{gd, extItem{Folder: true}, "folder"},
	} {
		if got := connectedPreview(c.ad, c.it, false); got != c.want {
			t.Fatalf("%+v: %s", c.it, got)
		}
	}
}

func TestConnectURL(t *testing.T) {
	if got := connectURL("files.google.drive", "/files/x"); got != "/settings/connections?capability=files.google.drive&return_to=%2Ffiles%2Fx" {
		t.Fatal(got)
	}
	if got := connectURL("files.google.drive", "//evil"); got != "/settings/connections?capability=files.google.drive" {
		t.Fatal(got)
	}
}
