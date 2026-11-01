// Package storage issues presigned upload/download URLs for S3 (or any
// S3-compatible store: R2, Tigris, MinIO). Without S3_BUCKET it falls back to
// local disk and the API serves /uploads/* itself (development only).
//
// Key layout: <purpose>/<owner uuid>/<random uuid>.<ext>. The owner is the
// uploading user (account deletion removes their prefix with DeleteOwner);
// content that must outlive its uploader (an org's drive) is keyed by the org
// and cleaned up by its module. Each purpose is
// registered once (RegisterPurpose) as public-read or private. Public objects
// are served from S3_PUBLIC_URL; private ones through short-lived presigned
// GETs after the owning module checks access. Keep the bucket policy in step:
// public prefixes public-read, everything else private (BucketPolicy;
// `server -bucket-policy` prints it).
package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/config"
)

// MaxUploadBytes is the default cap; a purpose may set a smaller or larger one.
const MaxUploadBytes = 10 << 20

var extByType = map[string]string{
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/heic":      "heic",
	"image/webp":      "webp",
	"image/gif":       "gif",
	"video/mp4":       "mp4",
	"video/quicktime": "mov",
	"audio/mp4":       "m4a",
	"audio/mpeg":      "mp3",
	"application/pdf": "pdf",
	"text/plain":      "txt",
	"text/csv":        "csv",
	"application/zip": "zip",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
}

// ImageTypes is the usual allow-list for photo purposes.
var ImageTypes = []string{"image/jpeg", "image/png", "image/heic", "image/webp"}

// DocumentTypes is the usual allow-list for file attachments.
var DocumentTypes = []string{
	"application/pdf", "text/plain", "text/csv", "application/zip",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

// Purpose describes one kind of upload.
type Purpose struct {
	Name     string
	Public   bool     // public-read under S3_PUBLIC_URL
	Types    []string // allowed content types (keys of extByType)
	MaxBytes int64    // 0 = MaxUploadBytes
	// ConsentExempt allows uploads before the user accepts the current legal
	// documents (e.g. attachments on a support ticket opened from the consent screen).
	ConsentExempt bool
	// AnyType accepts any well-formed content type instead of Types (a file
	// drive). Keys get the type's known extension or .bin. Serve such objects
	// with PresignGetWith and a Content-Disposition the owning module picks:
	// never inline for anything that can run script (HTML, SVG, XML).
	AnyType bool
}

var (
	purposesMu sync.RWMutex
	purposes   = map[string]Purpose{
		"avatar": {Name: "avatar", Public: true, Types: ImageTypes},
	}
)

// RegisterPurpose adds an upload purpose. Call it from a module's New.
func RegisterPurpose(p Purpose) {
	purposesMu.Lock()
	purposes[p.Name] = p
	purposesMu.Unlock()
}

func purpose(name string) (Purpose, bool) {
	purposesMu.RLock()
	defer purposesMu.RUnlock()
	p, ok := purposes[name]
	return p, ok
}

// IsConsentExempt reports whether purpose may be uploaded before consent.
func IsConsentExempt(name string) bool { p, _ := purpose(name); return p.ConsentExempt }

// IsPublic reports whether objects of purpose are publicly readable.
func IsPublic(name string) bool { p, _ := purpose(name); return p.Public }

// PublicPrefixes lists the key prefixes of public purposes ("avatar/", …),
// sorted: the only objects the bucket lets anyone read.
func PublicPrefixes() []string {
	purposesMu.RLock()
	defer purposesMu.RUnlock()
	var out []string
	for name, p := range purposes {
		if p.Public {
			out = append(out, name+"/")
		}
	}
	sort.Strings(out)
	return out
}

// BucketPolicy is the S3 bucket policy for the registered purposes:
// anonymous s3:GetObject on public prefixes only. Private objects and
// listing stay private (they go out as presigned URLs). `server
// -bucket-policy` prints it; dev-up applies it to MinIO.
func BucketPolicy(bucket string) []byte {
	var resources []string
	for _, p := range PublicPrefixes() {
		resources = append(resources, "arn:aws:s3:::"+bucket+"/"+p+"*")
	}
	type statement struct {
		Sid       string
		Effect    string
		Principal map[string][]string
		Action    []string
		Resource  []string
	}
	b, _ := json.MarshalIndent(struct {
		Version   string
		Statement []statement
	}{"2012-10-17", []statement{{
		Sid: "PublicPurposes", Effect: "Allow", Principal: map[string][]string{"AWS": {"*"}},
		Action: []string{"s3:GetObject"}, Resource: resources,
	}}}, "", "  ")
	return b
}

type Store struct {
	cfg     *config.Config
	client  *s3.Client
	presign *s3.PresignClient
	local   bool
	// localKey signs PresignGetWith URLs in local development (per process).
	localKey []byte
}

func New(ctx context.Context, cfg *config.Config) (*Store, error) {
	if cfg.S3Bucket == "" {
		// The local fallback serves every object (license documents and
		// report evidence included) to anyone at /uploads/*.
		if !cfg.IsDevelopment() {
			return nil, fmt.Errorf("S3_BUCKET is required outside development (local-disk uploads are development only)")
		}
		if err := os.MkdirAll(cfg.LocalUploadDir, 0o755); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		return &Store{cfg: cfg, local: true, localKey: key}, nil
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.S3Region)}
	if cfg.S3AccessKeyID != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			// S3-compatible stores lag AWS on the SDK's default CRC32
			// checksums: send and check them only where S3 requires them.
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
		o.UsePathStyle = cfg.S3ForcePathStyle
		// Objects uploaded through presigned PUTs carry no checksum; without
		// this every read of one prints an unstructured line to stderr.
		o.DisableLogOutputChecksumValidationSkipped = true
	})
	return &Store{cfg: cfg, client: client, presign: s3.NewPresignClient(client)}, nil
}

// Upload is a presigned PUT.
type Upload struct {
	Key       string            `json:"key"`
	UploadURL string            `json:"upload_url"`
	Headers   map[string]string `json:"headers"`
	PublicURL *string           `json:"public_url"`
}

// ValidatePurpose reports whether purpose/contentType/size are acceptable.
func ValidatePurpose(name, contentType string, size int64) error {
	p, ok := purpose(name)
	if !ok {
		return fmt.Errorf("unknown upload purpose")
	}
	if p.AnyType {
		if !validContentType(contentType) {
			return fmt.Errorf("unsupported file type")
		}
	} else {
		allowed := false
		for _, t := range p.Types {
			allowed = allowed || t == contentType
		}
		if _, known := extByType[contentType]; !allowed || !known {
			return fmt.Errorf("unsupported file type")
		}
	}
	limit := p.MaxBytes
	if limit == 0 {
		limit = MaxUploadBytes
	}
	if size <= 0 || size > limit {
		return fmt.Errorf("file must be %d MB or smaller", limit>>20)
	}
	return nil
}

// PresignPut returns a URL the client PUTs the file to.
func (s *Store) PresignPut(ctx context.Context, purpose string, owner uuid.UUID, contentType string, size int64) (*Upload, error) {
	if err := ValidatePurpose(purpose, contentType, size); err != nil {
		return nil, err
	}
	key := newKey(purpose, owner, contentType)
	up := &Upload{Key: key, Headers: map[string]string{"Content-Type": contentType}}
	if s.local {
		up.UploadURL = strings.TrimRight(s.cfg.PublicAPIURL, "/") + "/uploads/" + key
	} else {
		req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(s.cfg.S3Bucket),
			Key:           aws.String(key),
			ContentType:   aws.String(contentType),
			ContentLength: aws.Int64(size),
		}, s3.WithPresignExpires(15*time.Minute))
		if err != nil {
			return nil, err
		}
		up.UploadURL = req.URL
	}
	if IsPublic(purpose) {
		u := s.PublicURL(key)
		up.PublicURL = &u
	}
	return up, nil
}

// validContentType accepts "type/subtype" (parameters allowed) of sane length.
func validContentType(ct string) bool {
	if len(ct) > 127 {
		return false
	}
	mt, _, err := mime.ParseMediaType(ct)
	return err == nil && strings.Count(mt, "/") == 1 && !strings.HasPrefix(mt, "/") && !strings.HasSuffix(mt, "/")
}

func newKey(purpose string, owner uuid.UUID, contentType string) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	ext, ok := extByType[mt]
	if !ok {
		ext = "bin"
	}
	return fmt.Sprintf("%s/%s/%s.%s", purpose, owner, uuid.NewString(), ext)
}

// NewKey returns a fresh key for an object the server writes itself (Put,
// Copy) in the same layout as uploads: <purpose>/<owner>/<uuid>.<ext>.
func NewKey(purposeName string, owner uuid.UUID, contentType string) (string, error) {
	p, ok := purpose(purposeName)
	if !ok {
		return "", fmt.Errorf("unknown upload purpose")
	}
	if !p.AnyType {
		if _, known := extByType[contentType]; !known {
			return "", fmt.Errorf("unsupported file type")
		}
	} else if !validContentType(contentType) {
		return "", fmt.Errorf("unsupported file type")
	}
	return newKey(purposeName, owner, contentType), nil
}

// PublicURL is the read URL for a public object.
func (s *Store) PublicURL(key string) string {
	if s.local {
		return strings.TrimRight(s.cfg.PublicAPIURL, "/") + "/uploads/" + key
	}
	if s.cfg.S3PublicURL != "" {
		return strings.TrimRight(s.cfg.S3PublicURL, "/") + "/" + key
	}
	u, err := s.PresignGet(context.Background(), key, 24*time.Hour)
	if err != nil {
		return ""
	}
	return u
}

// PresignGet returns a time-limited read URL (private objects).
func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s.local {
		return strings.TrimRight(s.cfg.PublicAPIURL, "/") + "/uploads/" + key, nil
	}
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// GetOptions shape the response of a presigned read. S3 signs them into the
// URL, so a client can't change them.
type GetOptions struct {
	ContentType string // "" keeps the stored type
	Disposition string // `attachment; filename="report.pdf"` or `inline`; "" keeps the stored one
}

// PresignGetWith is PresignGet with a forced Content-Type and
// Content-Disposition: serve user files that could run script (HTML, SVG,
// unknown types) as attachments. Local development serves the same headers
// (signed with a per-process key) plus nosniff.
func (s *Store) PresignGetWith(ctx context.Context, key string, ttl time.Duration, o GetOptions) (string, error) {
	if s.local {
		q := url.Values{}
		if o.ContentType != "" {
			q.Set("response-content-type", o.ContentType)
		}
		if o.Disposition != "" {
			q.Set("response-content-disposition", o.Disposition)
		}
		q.Set("expires", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))
		q.Set("sig", s.localSig(key, q))
		return strings.TrimRight(s.cfg.PublicAPIURL, "/") + "/uploads/" + key + "?" + q.Encode(), nil
	}
	in := &s3.GetObjectInput{Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key)}
	if o.ContentType != "" {
		in.ResponseContentType = aws.String(o.ContentType)
	}
	if o.Disposition != "" {
		in.ResponseContentDisposition = aws.String(o.Disposition)
	}
	req, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *Store) localSig(key string, q url.Values) string {
	m := hmac.New(sha256.New, s.localKey)
	m.Write([]byte(key + "\n" + q.Get("response-content-type") + "\n" + q.Get("response-content-disposition") + "\n" + q.Get("expires")))
	return hex.EncodeToString(m.Sum(nil))
}

// ErrObjectNotFound is returned by Stat, Open and ReadPrefix for a missing key.
var ErrObjectNotFound = errors.New("storage: object not found")

// ObjectInfo is what Stat reports about a stored object.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Stat returns an object's size and stored content type: check an upload
// before trusting the size a client declared.
func (s *Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if strings.Contains(key, "..") {
		return ObjectInfo{}, fmt.Errorf("bad key")
	}
	if s.local {
		fi, err := os.Stat(filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(key)))
		if os.IsNotExist(err) {
			return ObjectInfo{}, ErrObjectNotFound
		}
		if err != nil {
			return ObjectInfo{}, err
		}
		return ObjectInfo{Size: fi.Size()}, nil
	}
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key)})
	if err != nil {
		return ObjectInfo{}, notFoundErr(err)
	}
	return ObjectInfo{Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType)}, nil
}

// Open streams an object. The caller closes it.
func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.Contains(key, "..") {
		return nil, fmt.Errorf("bad key")
	}
	if s.local {
		f, err := os.Open(filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(key)))
		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}
		return f, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, notFoundErr(err)
	}
	return out.Body, nil
}

// ReadPrefix returns up to n bytes from the start of an object (one ranged
// read): sniff a type, preview text.
func (s *Store) ReadPrefix(ctx context.Context, key string, n int64) ([]byte, error) {
	if n <= 0 {
		return []byte{}, nil
	}
	if s.local {
		rc, err := s.Open(ctx, key)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, n))
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key), Range: aws.String(fmt.Sprintf("bytes=0-%d", n-1)),
	})
	if err != nil {
		if apiErrorCode(err) == "InvalidRange" { // a ranged read of an empty object
			return []byte{}, nil
		}
		return nil, notFoundErr(err)
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, n))
}

// Put writes a small object the server produced (seed files, an edited text
// file). Uploads from clients go through PresignPut instead.
func (s *Store) Put(ctx context.Context, key, contentType string, body []byte) error {
	if strings.Contains(key, "..") {
		return fmt.Errorf("bad key")
	}
	if s.local {
		path := filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, body, 0o644)
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key), ContentType: aws.String(contentType),
		ContentLength: aws.Int64(int64(len(body))), Body: bytes.NewReader(body),
	})
	return err
}

// Copy duplicates an object inside the bucket (server side on S3).
func (s *Store) Copy(ctx context.Context, srcKey, dstKey string) error {
	if strings.Contains(srcKey, "..") || strings.Contains(dstKey, "..") {
		return fmt.Errorf("bad key")
	}
	if s.local {
		src, err := os.Open(filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(srcKey)))
		if os.IsNotExist(err) {
			return ErrObjectNotFound
		}
		if err != nil {
			return err
		}
		defer src.Close()
		path := filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(dstKey))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		dst, err := os.Create(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			return err
		}
		return dst.Close()
	}
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(dstKey),
		CopySource: aws.String(url.PathEscape(s.cfg.S3Bucket) + "/" + escapeKey(srcKey)),
	})
	return notFoundErr(err)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// notFoundErr maps a missing key to ErrObjectNotFound. HeadObject and
// GetObject return typed errors; CopyObject only the API code.
func notFoundErr(err error) error {
	var nf *s3types.NotFound
	var nk *s3types.NoSuchKey
	if errors.As(err, &nf) || errors.As(err, &nk) {
		return ErrObjectNotFound
	}
	switch apiErrorCode(err) {
	case "NoSuchKey", "NotFound":
		return ErrObjectNotFound
	}
	return err
}

// apiErrorCode is the S3 error code ("NoSuchKey", "InvalidRange") or "".
func apiErrorCode(err error) string {
	var api interface{ ErrorCode() string }
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}

// publicBase is the prefix of public object URLs ("" when public objects are
// served through presigned URLs, i.e. S3 without S3_PUBLIC_URL).
func (s *Store) publicBase() string {
	switch {
	case s.local:
		return strings.TrimRight(s.cfg.PublicAPIURL, "/") + "/uploads/"
	case s.cfg.S3PublicURL != "":
		return strings.TrimRight(s.cfg.S3PublicURL, "/") + "/"
	}
	return ""
}

// KeyFromPublicURL returns the object key behind one of our public URLs.
func (s *Store) KeyFromPublicURL(raw string) (string, bool) {
	base := s.publicBase()
	if base == "" {
		return "", false
	}
	key, ok := strings.CutPrefix(strings.TrimSpace(raw), base)
	if !ok || key == "" || strings.ContainsAny(key, "?#%\\") || strings.Contains(key, "..") {
		return "", false
	}
	return key, true
}

// clerkImageHosts serve Clerk profile images (accounts are provisioned with
// the Clerk image as their avatar). Add your provider's hosts if you swap.
var clerkImageHosts = map[string]bool{"img.clerk.com": true, "images.clerk.dev": true}

// AvatarURLAllowed reports whether raw may be stored as owner's avatar: the
// public URL of one of owner's avatar uploads (avatar/<owner>/…) or a Clerk
// profile image. https only, unless allowHTTP (development, where the API
// serves uploads over http). Callers also accept the value already on file.
func (s *Store) AvatarURLAllowed(raw string, owner uuid.UUID, allowHTTP bool) bool {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Host == "" || u.User != nil {
		return false
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowHTTP:
	default:
		return false
	}
	if clerkImageHosts[strings.ToLower(u.Hostname())] {
		return u.Scheme == "https"
	}
	key, ok := s.KeyFromPublicURL(raw)
	return ok && KeyOwnedBy(key, "avatar", owner)
}

// KeyOwnedBy reports whether key was issued for owner and purpose — call it
// before trusting a key a client hands back.
func KeyOwnedBy(key, purpose string, owner uuid.UUID) bool {
	return strings.HasPrefix(key, purpose+"/"+owner.String()+"/") && !strings.Contains(key, "..")
}

// Delete removes one object. Missing objects are not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if strings.Contains(key, "..") {
		return fmt.Errorf("bad key")
	}
	if s.local {
		err := os.Remove(filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(key)))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.cfg.S3Bucket), Key: aws.String(key)})
	return err
}

// DeleteOwner removes every object any purpose holds for owner. Account
// deletion calls it after the database transaction commits.
func (s *Store) DeleteOwner(ctx context.Context, owner uuid.UUID) error {
	purposesMu.RLock()
	names := make([]string, 0, len(purposes))
	for n := range purposes {
		names = append(names, n)
	}
	purposesMu.RUnlock()
	return s.deleteOwnerPurposes(ctx, owner, names)
}

// DeleteOwnerPurpose removes owner's objects under one purpose: uploads a
// user abandoned, or documents to drop right after a review.
func (s *Store) DeleteOwnerPurpose(ctx context.Context, owner uuid.UUID, purpose string) error {
	return s.deleteOwnerPurposes(ctx, owner, []string{purpose})
}

func (s *Store) deleteOwnerPurposes(ctx context.Context, owner uuid.UUID, names []string) error {
	for _, n := range names {
		prefix := n + "/" + owner.String() + "/"
		if s.local {
			if err := os.RemoveAll(filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(prefix))); err != nil {
				return err
			}
			continue
		}
		pager := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.cfg.S3Bucket), Prefix: aws.String(prefix)})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return err
			}
			for _, obj := range page.Contents {
				if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.cfg.S3Bucket), Key: obj.Key}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// largestPurposeBytes caps local PUTs at the biggest registered purpose
// (at least 100 MB).
func largestPurposeBytes() int64 {
	purposesMu.RLock()
	defer purposesMu.RUnlock()
	limit := int64(100 << 20)
	for _, p := range purposes {
		limit = max(limit, p.MaxBytes)
	}
	return limit
}

// Ping checks the bucket is reachable (readyz).
func (s *Store) Ping(ctx context.Context) error {
	if s.local {
		return nil
	}
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.cfg.S3Bucket)})
	return err
}

// LocalHandler serves PUT/GET /uploads/* in development (no S3).
func (s *Store) LocalHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.local {
			http.NotFound(w, r)
			return
		}
		rel, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/uploads/"))
		if err != nil || rel == "" || strings.Contains(rel, "..") {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		path := filepath.Join(s.cfg.LocalUploadDir, filepath.FromSlash(rel))
		switch r.Method {
		case http.MethodPut:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			f, err := os.Create(path)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			defer f.Close()
			if _, err := io.Copy(f, http.MaxBytesReader(w, r.Body, largestPurposeBytes())); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet, http.MethodHead:
			if q := r.URL.Query(); q.Has("sig") {
				// A PresignGetWith URL: check it like S3 would, then serve its headers.
				exp, err := strconv.ParseInt(q.Get("expires"), 10, 64)
				if err != nil || time.Now().Unix() > exp || !hmac.Equal([]byte(q.Get("sig")), []byte(s.localSig(rel, q))) {
					http.Error(w, "expired or invalid link", http.StatusForbidden)
					return
				}
				if ct := q.Get("response-content-type"); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				if cd := q.Get("response-content-disposition"); cd != "" {
					w.Header().Set("Content-Disposition", cd)
					if strings.HasPrefix(cd, "inline") {
						w.Header().Del("X-Frame-Options") // S3 sends none: inline PDFs preview in a frame
					}
				}
			}
			http.ServeFile(w, r, path)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}
