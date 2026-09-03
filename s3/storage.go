package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Put uploads r as key. S3 single-shot puts require the length up front;
// multi-part uploads for bigger objects are a deliberate non-feature here -
// document uploads are capped (25 MB) well below the 5 GB single-put limit.
func (c *Client) Put(ctx context.Context, bucket, key string, size int64, contentType string, r io.Reader) (string, error) {
	if err := c.checkBucket(bucket); err != nil {
		return "", err
	}
	in := &s3.PutObjectInput{
		Bucket:        &bucket,
		Key:           &key,
		Body:          r,
		ContentLength: &size,
	}
	if contentType != "" {
		in.ContentType = &contentType
	}
	out, err := c.api.PutObject(ctx, in)
	if err != nil {
		return "", wrap("put", bucket, key, err)
	}
	if out.ETag == nil {
		return "", nil
	}
	return *out.ETag, nil
}

// Get streams the object body: the caller owns closing it and should bound
// long downloads with the context.
func (c *Client) Get(ctx context.Context, bucket, key string) (FileBody, string, error) {
	if err := c.checkBucket(bucket); err != nil {
		return nil, "", err
	}
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return nil, "", wrap("get", bucket, key, err)
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	return out.Body, ct, nil
}

// Stat returns the object's size and content type without downloading it.
func (c *Client) Stat(ctx context.Context, bucket, key string) (int64, string, error) {
	if err := c.checkBucket(bucket); err != nil {
		return 0, "", err
	}
	out, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return 0, "", wrap("stat", bucket, key, err)
	}
	ct := ""
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	size := int64(0)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	return size, ct, nil
}

// Delete removes the object; S3 treats deleting a missing key as success.
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	if err := c.checkBucket(bucket); err != nil {
		return err
	}
	if _, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &bucket, Key: &key}); err != nil {
		return wrap("delete", bucket, key, err)
	}
	return nil
}

// PresignGet returns a short-lived GET URL, valid for ttl. Presigning is a
// local signature computation - it never touches the network and works even
// when the server is down; the URL only fails at fetch time. Filename, when
// set, is delivered as the browser's download filename.
func (c *Client) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration, filename string) (string, error) {
	if err := c.checkBucket(bucket); err != nil {
		return "", err
	}
	in := &s3.GetObjectInput{Bucket: &bucket, Key: &key}
	if filename != "" {
		disposition := fmt.Sprintf("attachment; filename=%q", filename)
		in.ResponseContentDisposition = &disposition
	}
	out, err := c.presigner.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrap("presign-get", bucket, key, err)
	}
	return out.URL, nil
}

// unwrapNotFound flattens the SDK's operation error into io.ErrNotFound-style
// handling callers can rely on: ErrNotFound wraps NoSuchKey/NotFound so
// callers check one sentinel instead of SDK internals.
var ErrNotFound = errors.New("object not found")

// wrap annotates SDK errors with the operation and object, and maps the two
// not-found shapes (GetObject's NoSuchKey, HeadObject's 404 "NotFound") to
// ErrNotFound so callers never import the SDK to check results.
func wrap(op, bucket, key string, err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NoSuchKey" || code == "NotFound" {
			return fmt.Errorf("s3 %s %s/%s: %w", op, bucket, key, ErrNotFound)
		}
	}
	return fmt.Errorf("s3 %s %s/%s: %w", op, bucket, key, err)
}
