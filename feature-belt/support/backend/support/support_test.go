package support

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestCheckAttachments(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	key := func(id uuid.UUID, ext string) string {
		return Purpose + "/" + id.String() + "/" + uuid.NewString() + "." + ext
	}

	rows, err := checkAttachments([]attachmentIn{
		{Key: key(owner, "png"), Name: "../../etc/screen.png", Size: 1200},
		{Key: key(owner, "pdf"), Name: ""},
	}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].name != "screen.png" || rows[0].contentType != "image/png" {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].name != "attachment.pdf" || rows[1].contentType != "application/pdf" {
		t.Fatalf("row 1 = %+v", rows[1])
	}

	bad := [][]attachmentIn{
		{{Key: key(other, "png")}},                              // someone else's upload
		{{Key: "avatar/" + owner.String() + "/x.png"}},          // wrong purpose
		{{Key: key(owner, "exe")}},                              // type not allowed
		{{Key: key(owner, "png"), Size: maxAttachmentSize + 1}}, // too big
		{{Key: Purpose + "/" + owner.String() + "/../x/y.png"}}, // traversal
		make([]attachmentIn, maxAttachments+1),                  // too many
	}
	k := key(owner, "png")
	bad = append(bad, []attachmentIn{{Key: k}, {Key: k}}) // duplicate
	for i, in := range bad {
		if _, err := checkAttachments(in, owner); err == nil {
			t.Errorf("case %d: accepted %+v", i, in)
		}
	}
}

func TestClientInfo(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-Client-Platform", "iOS")
	r.Header.Set("X-Client-Version", "1.4.0 (212)")
	if p, v := clientInfo(r); p != "ios" || v != "1.4.0 (212)" {
		t.Fatalf("got %q %q", p, v)
	}
	r.Header.Set("X-Client-Platform", "palm")
	r.Header.Set("X-Client-Version", "<script>")
	if p, v := clientInfo(r); p != "" || v != "" {
		t.Fatalf("malformed headers kept: %q %q", p, v)
	}
}

func TestLineAndParagraph(t *testing.T) {
	if got, err := line("Subject", "  App \n crashes  ", 3, 120); err != nil || got != "App crashes" {
		t.Fatalf("line = %q, %v", got, err)
	}
	if _, err := line("Subject", "ab", 3, 120); err == nil {
		t.Fatal("short subject accepted")
	}
	if got, err := paragraph("Message", "a\r\nb\x00c", 1, 10); err != nil || got != "a\nbc" {
		t.Fatalf("paragraph = %q, %v", got, err)
	}
}
