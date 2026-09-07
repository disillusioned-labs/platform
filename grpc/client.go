package grpc

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn *grpc.ClientConn
}

func NewClient(target string, opts ...Option) (*Client, error) {
	if target == "" {
		return nil, errors.New("grpc: target is required")
	}

	config := defaultConfig()

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(&config); err != nil {
			return nil, err
		}
	}

	dialOptions := []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(config.MaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(config.MaxSendMsgSize),
		),
	}

	if config.TLSConfig != nil {
		creds, err := ClientTLSCredentials(config.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("grpc: client TLS: %w", err)
		}

		dialOptions = append(
			dialOptions,
			grpc.WithTransportCredentials(creds),
		)
	} else {
		dialOptions = append(
			dialOptions,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}

	if config.Credentials != nil {
		dialOptions = append(
			dialOptions,
			grpc.WithPerRPCCredentials(config.Credentials),
		)
	}

	if config.KeepaliveParams != nil {
		dialOptions = append(
			dialOptions,
			grpc.WithKeepaliveParams(*config.KeepaliveParams),
		)
	}

	interceptors := make([]grpc.UnaryClientInterceptor, 0, len(config.UnaryClientInterceptors)+1)

	if config.UnaryTimeout > 0 {
		interceptors = append(
			interceptors,
			UnaryDefaultTimeout(config.UnaryTimeout),
		)
	}

	interceptors = append(
		interceptors,
		config.UnaryClientInterceptors...,
	)

	if len(interceptors) > 0 {
		dialOptions = append(
			dialOptions,
			grpc.WithChainUnaryInterceptor(interceptors...),
		)
	}

	if len(config.StreamClientInterceptors) > 0 {
		dialOptions = append(
			dialOptions,
			grpc.WithChainStreamInterceptor(
				config.StreamClientInterceptors...,
			),
		)
	}

	if config.TracerProvider != nil || config.MeterProvider != nil {
		handlerOptions := []otelgrpc.Option{}

		if config.TracerProvider != nil {
			handlerOptions = append(
				handlerOptions,
				otelgrpc.WithTracerProvider(config.TracerProvider),
			)
		}

		if config.MeterProvider != nil {
			handlerOptions = append(
				handlerOptions,
				otelgrpc.WithMeterProvider(config.MeterProvider),
			)
		}

		dialOptions = append(
			dialOptions,
			grpc.WithStatsHandler(
				otelgrpc.NewClientHandler(handlerOptions...),
			),
		)
	}

	conn, err := grpc.NewClient(target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("grpc: create client connection: %w", err)
	}

	return &Client{
		conn: conn,
	}, nil
}

func (c *Client) Conn() *grpc.ClientConn {
	return c.conn
}

func (c *Client) Ready(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return errors.New("grpc: client is nil")
	}

	c.conn.Connect()

	for {
		state := c.conn.GetState()

		if state == connectivity.Ready {
			return nil
		}

		if !c.conn.WaitForStateChange(ctx, state) {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("grpc: waiting for ready: %w", err)
			}

			return errors.New("grpc: connection state did not change")
		}
	}
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}

	return c.conn.Close()
}
