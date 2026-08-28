package main

import (
	"context"
	"log"
	"sync"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
)

type FleetServer struct {
	fleetpb.UnimplementedFleetServiceServer

	mu sync.Mutex

	trucks map[int32]*TruckInfo
}

type TruckInfo struct {
	Battery float64
	X       float64
	Y       float64
}

func NewFleetServer() *FleetServer {

	return &FleetServer{
		trucks: make(map[int32]*TruckInfo),
	}
}

func (s *FleetServer) RegisterTruck(ctx context.Context, req *fleetpb.RegisterRequest) (*fleetpb.RegisterResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	s.trucks[req.TruckId] = &TruckInfo{}

	log.Printf("Truck %d registered", req.TruckId)

	return &fleetpb.RegisterResponse{
		Accepted: true,
	}, nil
}

func (s *FleetServer) SendTelemetry(ctx context.Context, t *fleetpb.Telemetry) (*fleetpb.Command, error) {

	s.mu.Lock()

	if truck, ok := s.trucks[t.TruckId]; ok {
		truck.X = t.X
		truck.Y = t.Y
		truck.Battery = t.Battery
	}

	s.mu.Unlock()

	log.Printf(
		"Truck %d (%.1f, %.1f) Battery %.1f",
		t.TruckId,
		t.X,
		t.Y,
		t.Battery,
	)

	return &fleetpb.Command{
		Type: fleetpb.CommandType_CONTINUE,
	}, nil
}
