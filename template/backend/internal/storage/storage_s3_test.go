package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/config"
)

// These tests run the S3 code paths against a real S3-compatible server. They
// skip unless STORAGE_TEST_S3_ENDPOINT is set, e.g. with a local MinIO:
//
//	MINIO_ROOT_USER=testkey MINIO_ROOT_PASSWORD=testsecret123 minio server --address 127.0.0.1:29470 /tmp/minio
//	STORAGE_TEST_S3_ENDPOINT=http://127.0.0.1:29470 STORAGE_TEST_S3_KEY=testkey \
//	  STORAGE_TEST_S3_SECRET=testsecret123 go test ./internal/storage/
//
// Each run creates its own bucket and deletes it at the end.

func s3Store(t *testing.T) *Store {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_S3_ENDPOINT not set")
	}
	bucket := "storage-test-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	cfg := &config.Config{
		Env: "development", S3Endpoint: endpoint, S3Region: "us-east-1", S3Bucket: bucket,
		S3AccessKeyID: os.Getenv("STORAGE_TEST_S3_KEY"), S3SecretKey: os.Getenv("STORAGE_TEST_S3_SECRET"),
		S3ForcePathStyle: true, S3PublicURL: strings.TrimRight(endpoint, "/") + "/" + bucket,
	}
	ctx := context.Background()
	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pager := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				break
			}
			for _, o := range page.Contents {
				_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: o.Key})
			}
		}
		_, _ = s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	})
	RegisterPurpose(Purpose{Name: "test-private", Types: DocumentTypes})
	RegisterPurpose(Purpose{Name: "test-public", Public: true, Types: ImageTypes})
	RegisterPurpose(Purpose{Name: "test-any", AnyType: true, MaxBytes: 50 << 20})
	return s
}

func httpDo(t *testing.T, method, rawURL string, headers map[string]string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestS3PresignedUpload(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	owner := uuid.New()
	body := []byte("%PDF-1.4\n% a small pdf body for the test\n")

	up, err := s.PresignPut(ctx, "test-private", owner, "application/pdf", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if up.PublicURL != nil {
		t.Fatal("private purpose got a public URL")
	}
	if !KeyOwnedBy(up.Key, "test-private", owner) || !strings.HasSuffix(up.Key, ".pdf") {
		t.Fatalf("key %q", up.Key)
	}
	// What a browser sends: the returned headers plus its own Content-Length.
	if res, b := httpDo(t, http.MethodPut, up.UploadURL, up.Headers, body); res.StatusCode != http.StatusOK {
		t.Fatalf("presigned PUT: %d %s", res.StatusCode, b)
	}
	info, err := s.Stat(ctx, up.Key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(body)) || info.ContentType != "application/pdf" {
		t.Fatalf("stat %+v", info)
	}
	head, err := s.ReadPrefix(ctx, up.Key, 8)
	if err != nil || string(head) != "%PDF-1.4" {
		t.Fatalf("read prefix %q %v", head, err)
	}
	all, err := s.ReadPrefix(ctx, up.Key, 1<<20) // longer than the object
	if err != nil || !bytes.Equal(all, body) {
		t.Fatalf("read prefix past the end %q %v", all, err)
	}
	rc, err := s.Open(ctx, up.Key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("open %q", got)
	}
}

func TestS3PresignedUploadRejectsOtherTypeOrSize(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	body := []byte("hello")
	up, err := s.PresignPut(ctx, "test-private", uuid.New(), "text/plain", int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := httpDo(t, http.MethodPut, up.UploadURL, map[string]string{"Content-Type": "text/html"}, body); res.StatusCode == http.StatusOK {
		t.Fatal("PUT with another content type was accepted")
	}
	if res, _ := httpDo(t, http.MethodPut, up.UploadURL, up.Headers, []byte("hello, a longer body")); res.StatusCode == http.StatusOK {
		t.Fatal("PUT with another size was accepted")
	}
	if _, err := s.Stat(ctx, up.Key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("stat after rejected PUTs: %v", err)
	}
}

func TestS3EmptyObject(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	key, err := NewKey("test-any", uuid.New(), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, key, "text/plain", nil); err != nil {
		t.Fatal(err)
	}
	info, err := s.Stat(ctx, key)
	if err != nil || info.Size != 0 {
		t.Fatalf("stat %+v %v", info, err)
	}
	b, err := s.ReadPrefix(ctx, key, 8192)
	if err != nil || len(b) != 0 {
		t.Fatalf("read prefix of an empty object: %q %v", b, err)
	}
	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if len(got) != 0 {
		t.Fatalf("open %q", got)
	}
	dst, _ := NewKey("test-any", uuid.New(), "text/plain")
	if err := s.Copy(ctx, key, dst); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Stat(ctx, dst); err != nil || info.Size != 0 {
		t.Fatalf("copy of an empty object %+v %v", info, err)
	}
}

func TestS3MissingObject(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	key := "test-any/" + uuid.NewString() + "/missing.bin"
	if _, err := s.Stat(ctx, key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("stat: %v", err)
	}
	if _, err := s.Open(ctx, key); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.ReadPrefix(ctx, key, 10); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("read prefix: %v", err)
	}
	if err := s.Copy(ctx, key, "test-any/"+uuid.NewString()+"/x.bin"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("copy: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete of a missing key: %v", err)
	}
}

func TestS3CopyOddKeys(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	owner := uuid.New()
	for _, name := range []string{"plain.txt", "with space.txt", "plus+and%percent.txt", "résumé – 日本.txt", "q?hash#amp&.txt"} {
		src := "test-any/" + owner.String() + "/" + name
		body := []byte("copy of " + name)
		if err := s.Put(ctx, src, "text/plain", body); err != nil {
			t.Fatalf("%s: put: %v", name, err)
		}
		dst := "test-any/" + owner.String() + "/copy " + name
		if err := s.Copy(ctx, src, dst); err != nil {
			t.Fatalf("%s: copy: %v", name, err)
		}
		rc, err := s.Open(ctx, dst)
		if err != nil {
			t.Fatalf("%s: open copy: %v", name, err)
		}
		got, _ := io.ReadAll(rc)
		rc.Close()
		if !bytes.Equal(got, body) {
			t.Fatalf("%s: copy holds %q", name, got)
		}
		if info, err := s.Stat(ctx, dst); err != nil || info.ContentType != "text/plain" {
			t.Fatalf("%s: copy lost its content type: %+v %v", name, info, err)
		}
	}
}

func TestS3PresignGetWith(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	key, _ := NewKey("test-any", uuid.New(), "image/svg+xml")
	if err := s.Put(ctx, key, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"logo.svg", "my logo.svg", "logo «final» – 日本.svg", `quote"semi;.svg`} {
		cd := mime.FormatMediaType("attachment", map[string]string{"filename": name})
		u, err := s.PresignGetWith(ctx, key, time.Minute, GetOptions{ContentType: "application/octet-stream", Disposition: cd})
		if err != nil {
			t.Fatal(err)
		}
		res, _ := httpDo(t, http.MethodGet, u, nil, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: GET %d", name, res.StatusCode)
		}
		if got := res.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Fatalf("%s: content type %q", name, got)
		}
		if got := res.Header.Get("Content-Disposition"); got != cd {
			t.Fatalf("%s: disposition %q, want %q", name, got, cd)
		}
		_, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
		if err != nil || params["filename"] != name {
			t.Fatalf("%s: filename parsed back as %q (%v)", name, params["filename"], err)
		}
	}

	// Inline with a forced type.
	u, err := s.PresignGetWith(ctx, key, time.Minute, GetOptions{ContentType: "text/plain; charset=utf-8", Disposition: "inline"})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := httpDo(t, http.MethodGet, u, nil, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/plain; charset=utf-8" || res.Header.Get("Content-Disposition") != "inline" {
		t.Fatalf("inline: %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
	}

	// A client can't change the signed response headers.
	pu, _ := url.Parse(u)
	q := pu.Query()
	q.Set("response-content-type", "image/svg+xml")
	pu.RawQuery = q.Encode()
	if res, _ := httpDo(t, http.MethodGet, pu.String(), nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("tampered content type: %d", res.StatusCode)
	}
	q = pu.Query()
	q.Del("response-content-disposition")
	q.Del("response-content-type")
	pu.RawQuery = q.Encode()
	if res, _ := httpDo(t, http.MethodGet, pu.String(), nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("stripped overrides: %d", res.StatusCode)
	}
	// Nor read another object with the same signature.
	other, _ := NewKey("test-any", uuid.New(), "text/plain")
	_ = s.Put(ctx, other, "text/plain", []byte("other"))
	if res, _ := httpDo(t, http.MethodGet, strings.Replace(u, key, other, 1), nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("signature reused for another key: %d", res.StatusCode)
	}

	// Expiry.
	short, err := s.PresignGetWith(ctx, key, time.Second, GetOptions{Disposition: "attachment"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if res, _ := httpDo(t, http.MethodGet, short, nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("expired link: %d", res.StatusCode)
	}
}

func TestS3DeleteOwner(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	var mine, theirs []string
	for i := 0; i < 3; i++ {
		for _, p := range []string{"test-private", "test-any"} {
			k, _ := NewKey(p, owner, "text/plain")
			_ = s.Put(ctx, k, "text/plain", []byte("x"))
			mine = append(mine, k)
			k2, _ := NewKey(p, other, "text/plain")
			_ = s.Put(ctx, k2, "text/plain", []byte("y"))
			theirs = append(theirs, k2)
		}
	}
	if err := s.DeleteOwnerPurpose(ctx, owner, "test-private"); err != nil {
		t.Fatal(err)
	}
	for _, k := range mine {
		_, err := s.Stat(ctx, k)
		if strings.HasPrefix(k, "test-private/") != errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("after DeleteOwnerPurpose %s: %v", k, err)
		}
	}
	if err := s.DeleteOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	for _, k := range mine {
		if _, err := s.Stat(ctx, k); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("after DeleteOwner %s survived: %v", k, err)
		}
	}
	for _, k := range theirs {
		if _, err := s.Stat(ctx, k); err != nil {
			t.Fatalf("another owner's %s is gone: %v", k, err)
		}
	}
}

func TestS3BucketPolicy(t *testing.T) {
	s := s3Store(t)
	ctx := context.Background()
	if _, err := s.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(s.cfg.S3Bucket), Policy: aws.String(string(BucketPolicy(s.cfg.S3Bucket))),
	}); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	pub, _ := s.PresignPut(ctx, "test-public", owner, "image/png", 4)
	priv, _ := s.PresignPut(ctx, "test-private", owner, "text/plain", 4)
	for _, up := range []*Upload{pub, priv} {
		if res, b := httpDo(t, http.MethodPut, up.UploadURL, up.Headers, []byte("abcd")); res.StatusCode != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", up.Key, res.StatusCode, b)
		}
	}
	if pub.PublicURL == nil {
		t.Fatal("public purpose without a public URL")
	}
	if res, _ := httpDo(t, http.MethodGet, *pub.PublicURL, nil, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("public object, unsigned GET: %d", res.StatusCode)
	}
	if res, _ := httpDo(t, http.MethodGet, s.cfg.S3PublicURL+"/"+priv.Key, nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("private object, unsigned GET: %d", res.StatusCode)
	}
	if res, _ := httpDo(t, http.MethodGet, s.cfg.S3PublicURL+"?list-type=2", nil, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous listing: %d", res.StatusCode)
	}
	u, err := s.PresignGet(ctx, priv.Key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res, b := httpDo(t, http.MethodGet, u, nil, nil); res.StatusCode != http.StatusOK || string(b) != "abcd" {
		t.Fatalf("private object, presigned GET: %d", res.StatusCode)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}
