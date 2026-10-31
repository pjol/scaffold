// Package storage issues presigned upload/download URLs for S3 (or any
// S3-compatible store: R2, Tigris, MinIO). Without S3_BUCKET it falls back to
// local disk and the API serves /uploads/* itself (development only).
//
// Key layout: <purpose>/<owner uuid>/<random uuid>.<ext>. Each purpose is
// registered once (RegisterPurpose) as public-read or private. Public objects
// are served from S3_PUBLIC_URL; private ones through short-lived presigned
// GETs after the owning module checks access. Keep the bucket policy in step:
// public prefixes public-read, everything else private.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

type Store struct {
	cfg     *config.Config
	client  *s3.Client
	presign *s3.PresignClient
	local   bool
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
		return &Store{cfg: cfg, local: true}, nil
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
		}
		o.UsePathStyle = cfg.S3ForcePathStyle
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
	allowed := false
	for _, t := range p.Types {
		allowed = allowed || t == contentType
	}
	if _, known := extByType[contentType]; !allowed || !known {
		return fmt.Errorf("unsupported file type")
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
	key := fmt.Sprintf("%s/%s/%s.%s", purpose, owner, uuid.NewString(), extByType[contentType])
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
			if _, err := io.Copy(f, http.MaxBytesReader(w, r.Body, 100<<20)); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodGet, http.MethodHead:
			http.ServeFile(w, r, path)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}
