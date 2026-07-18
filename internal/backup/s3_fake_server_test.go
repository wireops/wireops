package backup

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wireops/wireops/internal/crypto"
)

// fakeS3Server is a minimal, in-memory S3-compatible HTTP server — just
// enough of the REST surface (path-style PUT/GET/DELETE + ListObjectsV2) for
// the real aws-sdk-go-v2 s3 client (internal/backup/remote.s3Storage) to
// talk to it end-to-end, without hitting real S3. Used to verify actual
// replication behavior (local copy survives, remote copy exists, List()
// flags both) through the real SDK request/response shapes.
type fakeS3Object struct {
	body []byte
	meta http.Header // x-amz-meta-* headers, as sent by the PUT request
}

type fakeS3Server struct {
	mu      sync.Mutex
	objects map[string]fakeS3Object // key: "/bucket/key"
	server  *httptest.Server
}

func newFakeS3Server() *fakeS3Server {
	f := &fakeS3Server{objects: map[string]fakeS3Object{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeS3Server) URL() string { return f.server.URL }
func (f *fakeS3Server) Close()      { f.server.Close() }

func (f *fakeS3Server) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		meta := make(http.Header)
		for k, v := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-amz-meta-") {
				meta[k] = v
			}
		}
		f.objects[r.URL.Path] = fakeS3Object{body: body, meta: meta}
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)

	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			f.serveList(w, r)
			return
		}
		obj, ok := f.objects[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for k, v := range obj.meta {
			w.Header()[k] = v
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(obj.body)

	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type listBucketResult struct {
	XMLName  xml.Name         `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name     string           `xml:"Name"`
	Prefix   string           `xml:"Prefix"`
	KeyCount int              `xml:"KeyCount"`
	Contents []listBucketItem `xml:"Contents"`
}

type listBucketItem struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	Size         int64  `xml:"Size"`
}

func (f *fakeS3Server) serveList(w http.ResponseWriter, r *http.Request) {
	// r.URL.Path for a bucket-level list is "/{bucket}"; keys are stored as
	// "/{bucket}/{key}".
	bucketPrefix := strings.TrimSuffix(r.URL.Path, "/") + "/"
	prefix := r.URL.Query().Get("prefix")

	result := listBucketResult{Prefix: prefix}
	for path, obj := range f.objects {
		if !strings.HasPrefix(path, bucketPrefix) {
			continue
		}
		key := strings.TrimPrefix(path, bucketPrefix)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		result.Contents = append(result.Contents, listBucketItem{
			Key:          key,
			LastModified: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			Size:         int64(len(obj.body)),
		})
	}
	result.KeyCount = len(result.Contents)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(result)
}

// objectCount returns how many objects currently exist, for test assertions.
func (f *fakeS3Server) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

// fakeS3Config builds an s3 integration config pointed at server, with its
// "secret" pre-encrypted under testS3SecretKey the same way a real save
// would (see SaveSettings/routes_register.go:encryptIntegrationConfig) —
// s3IntegrationConfig always tries to decrypt a non-empty "secret".
func fakeS3Config(t *testing.T, server *fakeS3Server, bucket string) map[string]any {
	t.Helper()
	encryptedSecret, err := crypto.Encrypt([]byte("fake-secret-key"), crypto.NormalizeSecretKey(testS3SecretKey))
	if err != nil {
		t.Fatalf("failed to encrypt fake secret: %v", err)
	}
	return map[string]any{
		"bucket":           bucket,
		"region":           "us-east-1",
		"endpoint":         server.URL(),
		"force_path_style": true,
		"encrypt_content":  false, // simplifies asserting the fake server's raw stored bytes
		"access_key":       "fake-access-key",
		"secret":           encryptedSecret,
	}
}
