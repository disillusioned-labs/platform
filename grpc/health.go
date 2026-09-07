package grpc

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type Health struct {
	server *health.Server
}

func NewHealth() *Health {
	return &Health{
		server: health.NewServer(),
	}
}

func (h *Health) Register(registrar grpc.ServiceRegistrar) {
	healthpb.RegisterHealthServer(registrar, h.server)
}

func (h *Health) Server() *health.Server {
	return h.server
}

func (h *Health) SetServing(service string) {
	h.server.SetServingStatus(
		service,
		healthpb.HealthCheckResponse_SERVING,
	)
}

func (h *Health) SetNotServing(service string) {
	h.server.SetServingStatus(
		service,
		healthpb.HealthCheckResponse_NOT_SERVING,
	)
}

func (h *Health) Shutdown() {
	h.server.Shutdown()
}
