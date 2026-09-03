// Package s3 owns the S3 object-storage client lifecycle (AWS SDK for Go v2),
// analogous to redis for the cache and postgres for the database pool. It
// works against real AWS S3 and any S3-compatible endpoint (MinIO for local
// development). Consumers of the client (document storage, OCR file source)
// hold the Store interface; tests substitute a fake.
//
// The client is instrumented for OpenTelemetry traces and metrics via the
// otelaws middleware, reading the global providers like redisotel/otelpgx do
// - so telemetry.Setup must run before New.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// Config holds everything New needs. Region is always required (S3-compatible
// servers need a placeholder such as "us-east-1"); credentials are either
// static (MinIO/local dev) or resolved from the default AWS chain
// (environment, IMDSv2 on EC2/EKS) when AccessKeyID is empty.
type Config struct {
	// Endpoint is the S3-compatible API endpoint (e.g. http://localhost:9000
	// for MinIO). Empty means real AWS.
	Endpoint string
	// Region is the signing region. Always required.
	Region string
	// AccessKeyID / SecretAccessKey are static credentials for
	// S3-compatible servers. Leave AccessKeyID empty to use the default AWS
	// credential chain instead.
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	// UsePathStyle addresses buckets as
	// endpoint/bucket/key instead of bucket.endpoint/key. Required for
	// MinIO/local endpoints; on real AWS, virtual-host style is preferred.
	UsePathStyle bool

	// BucketAllowlist restricts every operation to the listed buckets (boot
	// is not blocked when it is empty, but any non-listed bucket is refused
	// at call time). The OCR contract requires the engine's allowlist to be
	// non-empty; storage services set their own policy on top.
	BucketAllowlist []string

	// RetryMaxAttempts is the per-operation retry budget. 0 means the SDK
	// default (3).
	RetryMaxAttempts int
	// RetryMode is "standard" (default) or "adaptive". Adaptive adds a
	// client-side rate limiter that can delay the initial request when the
	// service throttles - useful for heavy uploads, overkill for low volume.
	RetryMode string

	// OperationTimeout bounds each API call's connection and response-header
	// phases. The object body of a Get streams outside this timeout: bound
	// long downloads with the caller's context instead. 0 means 10s.
	OperationTimeout time.Duration
}

// ErrBucketNotAllowed is returned when an operation targets a bucket outside
// Config.BucketAllowlist. It is a configuration bug, not a transient error.
var ErrBucketNotAllowed = errors.New("bucket is not in the allowlist")

// Validate rejects configurations that would fail confusingly at first use.
func (c Config) Validate() error {
	if c.Region == "" {
		return fmt.Errorf("s3.region must not be empty (use e.g. us-east-1 for MinIO)")
	}
	if (c.AccessKeyID == "") != (c.SecretAccessKey == "") {
		return fmt.Errorf("s3 credentials must be both set or both empty (empty means the default AWS credential chain)")
	}
	switch c.RetryMode {
	case "", string(aws.RetryModeStandard), string(aws.RetryModeAdaptive):
	default:
		return fmt.Errorf("s3.retry_mode must be standard or adaptive, got %q", c.RetryMode)
	}
	return nil
}

// Store is the object-storage contract services depend on. Get streams the
// object body: the caller owns closing it and bounding long downloads with
// the context.
type Store interface {
	// Put uploads r as key. ContentLength must be known up front (S3
	// requirement for single-shot puts); ContentType is optional. The SDK
	// closes r; callers that own a closer keep it.
	Put(ctx context.Context, bucket, key string, size int64, contentType string, r io.Reader) (etag string, err error)
	// Get returns the object body and its content type. The caller owns
	// closing the body.
	Get(ctx context.Context, bucket, key string) (body FileBody, contentType string, err error)
	// Stat returns the object's size and content type.
	Stat(ctx context.Context, bucket, key string) (size int64, contentType string, err error)
	// Delete removes the object; deleting a missing object is not an error.
	Delete(ctx context.Context, bucket, key string) error
	// PresignGet returns a short-lived GET URL, valid for ttl. Filename, when
	// set, is delivered as a download filename (ResponseContentDisposition).
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration, filename string) (url string, err error)
}

// FileBody is a reader-closer returned by Get; io.ReadCloser on S3's stream.
type FileBody interface {
	Read(p []byte) (int, error)
	Close() error
}

var _ Store = (*Client)(nil)

// Client wraps the S3 API client with bucket-allowlist enforcement.
type Client struct {
	api       *s3.Client
	presigner *s3.PresignClient
	allow     map[string]struct{}
}

// New builds the client and instruments it for OpenTelemetry (a span per
// operation plus client-side metrics - duration, retries, status - via the
// global providers). It performs no network calls: connectivity problems
// surface at first use (presigning, notably, never touches the network), so
// call Ping explicitly when boot must fail on an unreachable server.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
		config.WithRetryMaxAttempts(cfg.RetryMaxAttempts),
	}
	if cfg.RetryMode != "" {
		loadOpts = append(loadOpts, config.WithRetryMode(aws.RetryMode(cfg.RetryMode)))
	}
	if cfg.AccessKeyID != "" {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	awsCfg.HTTPClient = &http.Client{
		Transport: hardenedTransport(),
		Timeout:   orDefault(cfg.OperationTimeout, 10*time.Second),
	}

	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = cfg.UsePathStyle
			// The OTel middleware reads the global tracer/meter providers -
			// telemetry.Setup must have run before New. S3AttributeBuilder
			// adds the bucket and key to every span.
			otelaws.AppendMiddlewares(&o.APIOptions,
				otelaws.WithAttributeBuilder(otelaws.S3AttributeBuilder))
		},
	}
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) { o.BaseEndpoint = &cfg.Endpoint })
		// S3-compatible servers (MinIO) lag behind the newest integrity
		// features; SDK-v2 clients newer than 2023 send CRC32/CRC64NVME
		// checksums by default, which such servers may reject. WhenRequired
		// keeps checksums for operations that need them without breaking
		// compatibility.
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		})
	}

	api := s3.NewFromConfig(awsCfg, s3Opts...)

	allow := make(map[string]struct{}, len(cfg.BucketAllowlist))
	for _, b := range cfg.BucketAllowlist {
		allow[b] = struct{}{}
	}

	return &Client{
		api:       api,
		presigner: s3.NewPresignClient(api),
		allow:     allow,
	}, nil
}

// Ping verifies credentials and (when bucket is set) that the bucket exists
// and is readable. Boot should fail when this fails - an object-storage
// outage is not something the service can degrade around.
func (c *Client) Ping(ctx context.Context, bucket string) error {
	if bucket != "" {
		if err := c.checkBucket(bucket); err != nil {
			return err
		}
		if _, err := c.api.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket}); err != nil {
			return fmt.Errorf("head bucket %q: %w", bucket, err)
		}
		return nil
	}
	// No bucket configured: prove credentials/endpoint with a list call.
	if _, err := c.api.ListBuckets(ctx, &s3.ListBucketsInput{}); err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}
	return nil
}

func (c *Client) checkBucket(bucket string) error {
	if len(c.allow) == 0 {
		return nil
	}
	if _, ok := c.allow[bucket]; !ok {
		return fmt.Errorf("%w: %q", ErrBucketNotAllowed, bucket)
	}
	return nil
}

func orDefault(d time.Duration, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// hardenedTransport bounds the connection and header phases of every call
// (fast failure against an unreachable server) while leaving body streaming
// unbounded - Get downloads are cut by the caller's context, not the clock.
func hardenedTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 5 * time.Second
	t.ResponseHeaderTimeout = 30 * time.Second
	t.IdleConnTimeout = 90 * time.Second
	t.MaxIdleConnsPerHost = 25
	return t
}
