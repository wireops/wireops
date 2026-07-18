package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeObject struct {
	body []byte
	meta map[string]string
}

type fakeS3 struct {
	objects  map[string]fakeObject
	putCalls int
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: map[string]fakeObject{}}
}

func (f *fakeS3) PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.putCalls++
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(in.Key)] = fakeObject{body: data, meta: in.Metadata}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	obj, ok := f.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(obj.body)), Metadata: obj.meta}, nil
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)
	var contents []types.Object
	for k, v := range f.objects {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		size := int64(len(v.body))
		contents = append(contents, types.Object{Key: aws.String(k), Size: aws.Int64(size)})
		if in.MaxKeys != nil && int32(len(contents)) >= *in.MaxKeys {
			break
		}
	}
	return &s3.ListObjectsV2Output{Contents: contents}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(f.objects, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func TestS3StoragePutJoinsPrefix(t *testing.T) {
	fake := newFakeS3()
	st := &s3Storage{api: fake, bucket: "b", prefix: "wireops"}

	if err := st.Put(context.Background(), "backup1.zip", strings.NewReader("data"), 4, nil); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if _, ok := fake.objects["wireops/backup1.zip"]; !ok {
		t.Fatalf("expected key %q in fake store, got %+v", "wireops/backup1.zip", fake.objects)
	}
}

func TestS3StorageListStripsPrefixAndSkipsMarker(t *testing.T) {
	fake := newFakeS3()
	st := &s3Storage{api: fake, bucket: "b", prefix: "wireops"}

	fake.objects["wireops/"] = fakeObject{} // prefix marker
	fake.objects["wireops/backup1.zip"] = fakeObject{body: []byte("1234")}
	fake.objects["other/backup2.zip"] = fakeObject{body: []byte("12")} // different prefix, must not show up

	objs, err := st.List(context.Background())
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("expected 1 object, got %d: %+v", len(objs), objs)
	}
	if objs[0].Key != "backup1.zip" || objs[0].Size != 4 {
		t.Fatalf("unexpected object: %+v", objs[0])
	}
}

func TestS3StorageEnsurePrefixCreatesMarkerOnlyWhenMissing(t *testing.T) {
	fake := newFakeS3()
	st := &s3Storage{api: fake, bucket: "b", prefix: "wireops"}

	if err := st.EnsurePrefix(context.Background()); err != nil {
		t.Fatalf("EnsurePrefix failed: %v", err)
	}
	if fake.putCalls != 1 {
		t.Fatalf("expected 1 PutObject call to create the marker, got %d", fake.putCalls)
	}
	if _, ok := fake.objects["wireops/"]; !ok {
		t.Fatal("expected marker object to be created")
	}

	if err := st.EnsurePrefix(context.Background()); err != nil {
		t.Fatalf("second EnsurePrefix failed: %v", err)
	}
	if fake.putCalls != 1 {
		t.Fatalf("expected no additional PutObject call once the marker exists, got %d total", fake.putCalls)
	}
}

func TestS3StorageEnsurePrefixNoopWhenPrefixEmpty(t *testing.T) {
	fake := newFakeS3()
	st := &s3Storage{api: fake, bucket: "b", prefix: ""}

	if err := st.EnsurePrefix(context.Background()); err != nil {
		t.Fatalf("EnsurePrefix failed: %v", err)
	}
	if fake.putCalls != 0 {
		t.Fatalf("expected no marker for an empty prefix, got %d PutObject calls", fake.putCalls)
	}
}

func TestS3StorageDeleteUsesFullKey(t *testing.T) {
	fake := newFakeS3()
	fake.objects["wireops/backup1.zip"] = fakeObject{body: []byte("data")}
	st := &s3Storage{api: fake, bucket: "b", prefix: "wireops"}

	if err := st.Delete(context.Background(), "backup1.zip"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := fake.objects["wireops/backup1.zip"]; ok {
		t.Fatal("expected object to be deleted")
	}
}

func TestNewS3StorageRequiresCredentials(t *testing.T) {
	cases := []map[string]any{
		{"region": "us-east-1", "bucket": "b"},
		{"bucket": "b", "region": "us-east-1"},
	}
	for _, cfg := range cases {
		if _, err := newS3Storage(cfg, map[string]any{}); err == nil {
			t.Fatalf("expected error for incomplete config/credentials: %+v", cfg)
		}
	}

	st, err := newS3Storage(
		map[string]any{"bucket": "b", "region": "us-east-1"},
		map[string]any{"access_key": "ak", "secret_key": "sk"},
	)
	if err != nil {
		t.Fatalf("expected valid config to succeed: %v", err)
	}
	if st == nil {
		t.Fatal("expected non-nil storage")
	}
}
