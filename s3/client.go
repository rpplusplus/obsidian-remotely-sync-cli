package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// RemoteFile represents an object in S3.
type RemoteFile struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Client wraps the S3 client for Tencent Cloud COS.
type Client struct {
	s3     *s3.Client
	bucket string
	prefix string
}

// NewClient creates a new S3 client configured for Tencent Cloud COS.
func NewClient(ctx context.Context, endpoint, region, accessKey, secretKey, bucket, prefix string) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(awscreds.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = false // Tencent Cloud COS uses virtual-hosted-style
	})

	return &Client{
		s3:     s3Client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (c *Client) fullKey(key string) string {
	if c.prefix == "" {
		return key
	}
	return c.prefix + "/" + key
}

func (c *Client) stripPrefix(key string) string {
	if c.prefix == "" {
		return key
	}
	pfx := c.prefix + "/"
	if len(key) > len(pfx) && key[:len(pfx)] == pfx {
		return key[len(pfx):]
	}
	return key
}

// ListObjects lists all objects in the bucket (under the configured prefix).
// Skips keys ending with '/' (folder markers).
func (c *Client) ListObjects(ctx context.Context) ([]RemoteFile, error) {
	var files []RemoteFile
	var continuationToken *string

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(c.prefix),
			ContinuationToken: continuationToken,
		}

		resp, err := c.s3.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("listing objects: %w", err)
		}

		for _, obj := range resp.Contents {
			key := aws.ToString(obj.Key)
			// Skip folder markers
			if len(key) > 0 && key[len(key)-1] == '/' {
				continue
			}
			files = append(files, RemoteFile{
				Key:          c.stripPrefix(key),
				Size:         aws.ToInt64(obj.Size),
				LastModified: aws.ToTime(obj.LastModified),
			})
		}

		if !aws.ToBool(resp.IsTruncated) {
			break
		}
		continuationToken = resp.NextContinuationToken
	}

	return files, nil
}

// GetObject downloads an object by key. Returns the raw encrypted bytes.
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	resp, err := c.s3.GetObject(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("getting object %q: %w", key, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading object body %q: %w", key, err)
	}

	return data, nil
}

// PutObject uploads data to the given key.
func (c *Client) PutObject(ctx context.Context, key string, data []byte) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
		Body:   bytes.NewReader(data),
	}

	_, err := c.s3.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("putting object %q: %w", key, err)
	}

	return nil
}

// DeleteObject deletes an object by key.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.fullKey(key)),
	}

	_, err := c.s3.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("deleting object %q: %w", key, err)
	}

	return nil
}
