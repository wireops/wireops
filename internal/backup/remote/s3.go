package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func init() {
	Register("s3", newS3Storage)
}

// s3API is the narrow subset of *s3.Client this package calls, so tests can
// inject a fake instead of talking to real S3.
type s3API interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

var _ s3API = (*s3.Client)(nil)

type s3Storage struct {
	api    s3API
	bucket string
	prefix string
}

func newS3Storage(config map[string]any, creds map[string]any) (Storage, error) {
	bucket := strFrom(config, "bucket")
	region := strFrom(config, "region")
	endpoint := strFrom(config, "endpoint")
	prefix := strings.Trim(strFrom(config, "prefix"), "/")
	forcePathStyle := boolFrom(config, "force_path_style")

	accessKey := strFrom(creds, "access_key")
	secretKey := strFrom(creds, "secret_key")

	if bucket == "" || region == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("remote/s3: bucket, region, access_key and secret_key are required")
	}

	client := s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		UsePathStyle: forcePathStyle,
		BaseEndpoint: nonEmptyPtr(endpoint),
	})

	return &s3Storage{api: client, bucket: bucket, prefix: prefix}, nil
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strFrom(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func boolFrom(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

// fullKey joins the configured prefix onto a bare backup key. S3 keys
// always use "/" regardless of host OS, so path.Join (not filepath.Join) is
// used here.
func (s *s3Storage) fullKey(key string) string {
	return path.Join(s.prefix, key)
}

// markerKey is the zero-byte object EnsurePrefix writes so the prefix is
// visible in listings/UIs even before a real backup exists under it.
func (s *s3Storage) markerKey() string {
	return s.prefix + "/"
}

func (s *s3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, meta map[string]string) error {
	_, err := s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           aws.String(s.fullKey(key)),
		Body:          r,
		ContentLength: aws.Int64(size),
		Metadata:      meta,
	})
	if err != nil {
		return fmt.Errorf("remote/s3: put %q: %w", key, err)
	}
	return nil
}

func (s *s3Storage) Get(ctx context.Context, key string) (io.ReadCloser, map[string]string, error) {
	out, err := s.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("remote/s3: get %q: %w", key, err)
	}
	return out.Body, out.Metadata, nil
}

func (s *s3Storage) List(ctx context.Context) ([]Info, error) {
	var out []Info
	var token *string
	listPrefix := s.prefix
	if listPrefix != "" {
		listPrefix += "/"
	}
	marker := s.markerKey()

	for {
		resp, err := s.api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            &s.bucket,
			Prefix:            aws.String(listPrefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("remote/s3: list: %w", err)
		}
		for _, obj := range resp.Contents {
			key := aws.ToString(obj.Key)
			if key == marker {
				continue // prefix marker object, not a real backup
			}
			bareKey := strings.TrimPrefix(key, listPrefix)
			if bareKey == "" {
				continue
			}
			info := Info{Key: bareKey, Size: aws.ToInt64(obj.Size)}
			if obj.LastModified != nil {
				info.Modified = *obj.LastModified
			}
			out = append(out, info)
		}
		if resp.NextContinuationToken == nil {
			break
		}
		token = resp.NextContinuationToken
	}
	return out, nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		return fmt.Errorf("remote/s3: delete %q: %w", key, err)
	}
	return nil
}

func (s *s3Storage) EnsurePrefix(ctx context.Context) error {
	if s.prefix == "" {
		return nil
	}
	resp, err := s.api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  &s.bucket,
		Prefix:  aws.String(s.prefix + "/"),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return fmt.Errorf("remote/s3: check prefix: %w", err)
	}
	if len(resp.Contents) > 0 {
		return nil
	}
	_, err = s.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &s.bucket,
		Key:           aws.String(s.markerKey()),
		Body:          strings.NewReader(""),
		ContentLength: aws.Int64(0),
	})
	if err != nil {
		return fmt.Errorf("remote/s3: create prefix marker: %w", err)
	}
	return nil
}

func (s *s3Storage) Close() error {
	return nil
}
