package blob

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures an S3-compatible store (AWS, R2, Hetzner, MinIO).
type S3Config struct {
	Endpoint  string // URL, e.g. https://<account>.r2.cloudflarestorage.com
	Region    string
	Bucket    string
	Prefix    string // optional key prefix inside the bucket
	AccessKey string
	SecretKey string
}

// S3 stores blobs in a bucket.
type S3 struct {
	client *minio.Client
	bucket string
	prefix string
}

var _ Store = (*S3)(nil)

// NewS3 returns an S3 store. It does not contact the server.
func NewS3(c S3Config) (*S3, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("blob: s3 endpoint %q must be a URL", c.Endpoint)
	}
	if c.Bucket == "" {
		return nil, fmt.Errorf("blob: s3 bucket is required")
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: u.Scheme != "http",
		Region: c.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("blob: s3 client: %w", err)
	}
	return &S3{client: client, bucket: c.Bucket, prefix: strings.Trim(c.Prefix, "/")}, nil
}

func (s *S3) object(key string) (string, error) {
	if err := CheckKey(key); err != nil {
		return "", err
	}
	if s.prefix == "" {
		return key, nil
	}
	return s.prefix + "/" + key, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	if _, err := s.client.PutObject(ctx, s.bucket, obj, r, size, minio.PutObjectOptions{}); err != nil {
		return fmt.Errorf("blob: put %s: %w", key, err)
	}
	return nil
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	obj, err := s.object(key)
	if err != nil {
		return nil, Info{}, err
	}
	info, err := s.Stat(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	o, err := s.client.GetObject(ctx, s.bucket, obj, minio.GetObjectOptions{})
	if err != nil {
		return nil, Info{}, fmt.Errorf("blob: get %s: %w", key, err)
	}
	return o, info, nil
}

func (s *S3) Stat(ctx context.Context, key string) (Info, error) {
	obj, err := s.object(key)
	if err != nil {
		return Info{}, err
	}
	st, err := s.client.StatObject(ctx, s.bucket, obj, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return Info{}, ErrNotFound
		}
		return Info{}, fmt.Errorf("blob: stat %s: %w", key, err)
	}
	return Info{Size: st.Size, ModTime: st.LastModified}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, obj, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("blob: delete %s: %w", key, err)
	}
	return nil
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	obj, err := s.object(prefix)
	if err != nil {
		return err
	}
	objects := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: obj + "/", Recursive: true})
	for e := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		if e.Err != nil {
			return fmt.Errorf("blob: delete %s: %w", prefix, e.Err)
		}
	}
	return nil
}
