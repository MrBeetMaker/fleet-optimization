package main

import (
	"context"
	"log"
	"sync"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
)

type TruckState int

var StateName = map[int32]string{
	0: "idle",
	1: "driving",
	2: "charging",
	3: "waiting",
	4: "delivering",
}

type FleetServer struct {
	fleetpb.UnimplementedFleetServiceServer

	mu sync.Mutex

	world map[int32]*fleetpb.Point

	trucks map[int32]*TruckInfo
}

type TruckInfo struct {
	Battery float64
	State   int32
	X       float64
	Y       float64
}

func NewFleetServer() *FleetServer {

	return &FleetServer{
		trucks: make(map[int32]*TruckInfo),
		world:  map[int32]*fleetpb.Point{1: {X: 1, Y: 2}},
	}
}

func (s *FleetServer) RegisterTruck(ctx context.Context, req *fleetpb.RegisterRequest) (*fleetpb.RegisterResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.trucks[req.TruckId]; exists {
		log.Printf("Declined registration request for Truck %d: Truck %d is already registered.", req.TruckId, req.TruckId)

		return &fleetpb.RegisterResponse{
			Accepted: false,
			Points:   map[int32]*fleetpb.Point{},
		}, nil
	}

	s.trucks[req.TruckId] = &TruckInfo{}

	log.Printf("Truck %d registered", req.TruckId)

	return &fleetpb.RegisterResponse{
		Accepted: true,
		Points:   s.world,
	}, nil
}

func (s *FleetServer) SendTelemetry(ctx context.Context, t *fleetpb.Telemetry) (*fleetpb.Command, error) {

	s.mu.Lock()

	if truck, ok := s.trucks[t.TruckId]; ok {
		truck.X = t.X
		truck.Y = t.Y
		truck.State = t.State
		truck.Battery = t.Battery
	}

	s.mu.Unlock()

	log.Printf(
		"Truck %d (%.1f, %.1f) Battery %.1f  (%s)",
		t.TruckId,
		t.X,
		t.Y,
		t.Battery,
		StateName[t.State],
	)

	if t.X == 20 && t.Y == 10 {
		return &fleetpb.Command{
			Type: fleetpb.CommandType_STOP,
		}, nil
	}

	return &fleetpb.Command{
		Type: fleetpb.CommandType_CONTINUE,
	}, nil
}
