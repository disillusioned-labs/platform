package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

type Server struct {
	server *grpc.Server
	health *Health
	logger Logger
}

func NewServer(opts ...Option) (*Server, error) {
	config := defaultConfig()

	for _, opt := range opts {
		if opt == nil {
			continue
		}

		if err := opt(&config); err != nil {
			return nil, err
		}
	}

	var serverOptions []grpc.ServerOption

	serverOptions = append(
		serverOptions,
		grpc.MaxRecvMsgSize(config.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(config.MaxSendMsgSize),
		grpc.MaxHeaderListSize(uint32(config.MaxHeaderSize)),
	)

	if config.TLSConfig != nil {
		creds, err := ServerTLSCredentials(config.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("grpc: server TLS: %w", err)
		}

		serverOptions = append(
			serverOptions,
			grpc.Creds(creds),
		)
	} else {
		// Server intentionally allows insecure transport only when
		// explicitly configured without TLS. This is useful for tests
		// and local development.
	}

	if config.ServerKeepalive != nil {
		serverOptions = append(
			serverOptions,
			grpc.KeepaliveParams(*config.ServerKeepalive),
		)
	}

	if config.KeepaliveEnforcement != nil {
		serverOptions = append(
			serverOptions,
			grpc.KeepaliveEnforcementPolicy(
				*config.KeepaliveEnforcement,
			),
		)
	}

	unaryInterceptors := append(
		[]grpc.UnaryServerInterceptor{
			UnaryRecovery(config.Logger),
		},
		config.UnaryServerInterceptors...,
	)

	if len(unaryInterceptors) > 0 {
		serverOptions = append(
			serverOptions,
			grpc.ChainUnaryInterceptor(unaryInterceptors...),
		)
	}

	streamInterceptors := append(
		[]grpc.StreamServerInterceptor{
			StreamRecovery(config.Logger),
		},
		config.StreamServerInterceptors...,
	)

	if len(streamInterceptors) > 0 {
		serverOptions = append(
			serverOptions,
			grpc.ChainStreamInterceptor(streamInterceptors...),
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

		serverOptions = append(
			serverOptions,
			grpc.StatsHandler(
				otelgrpc.NewServerHandler(handlerOptions...),
			),
		)
	}

	server := grpc.NewServer(serverOptions...)

	health := NewHealth()
	health.Register(server)

	return &Server{
		server: server,
		health: health,
		logger: config.Logger,
	}, nil
}

func (s *Server) GRPC() *grpc.Server {
	return s.server
}

func (s *Server) Health() *Health {
	return s.health
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("grpc: listener is nil")
	}

	s.health.SetServing("")

	if err := s.server.Serve(listener); err != nil {
		return fmt.Errorf("grpc: serve: %w", err)
	}

	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}

	if ctx == nil {
		return errors.New("grpc: shutdown context is nil")
	}

	s.health.Shutdown()

	done := make(chan struct{})

	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil

	case <-ctx.Done():
		s.server.Stop()

		return fmt.Errorf(
			"grpc: graceful shutdown: %w",
			ctx.Err(),
		)
	}
}
