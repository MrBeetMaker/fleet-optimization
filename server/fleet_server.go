package main

import (
	"context"
	"log"
	"math/rand"
	"sync"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
)

type FleetServer struct {
	fleetpb.UnimplementedFleetServiceServer

	mu sync.Mutex

	world map[int32]*fleetpb.Point

	trucks map[int32]*TruckInfo
}

type TruckInfo struct {
	Battery float64
	State   fleetpb.TruckState
	X       float64
	Y       float64
}

func NewFleetServer() *FleetServer {

	return &FleetServer{
		trucks: make(map[int32]*TruckInfo),
		world: map[int32]*fleetpb.Point{
			1:  {X: float32(1.0), Y: float32(1.0)},
			2:  {X: float32(1.8), Y: float32(1.4)},
			3:  {X: float32(2.5), Y: float32(2.1)},
			4:  {X: float32(1.6), Y: float32(2.6)},
			5:  {X: float32(0.8), Y: float32(2.0)},
			6:  {X: float32(2.1), Y: float32(0.7)},
			7:  {X: float32(3.0), Y: float32(1.2)},
			8:  {X: float32(3.4), Y: float32(2.3)},
			9:  {X: float32(7.0), Y: float32(1.0)},
			10: {X: float32(8.2), Y: float32(1.3)},
			11: {X: float32(8.8), Y: float32(2.2)},
			12: {X: float32(8.1), Y: float32(3.0)},
			13: {X: float32(6.9), Y: float32(3.2)},
			14: {X: float32(6.1), Y: float32(2.3)},
			15: {X: float32(6.2), Y: float32(1.4)},
			16: {X: float32(7.5), Y: float32(2.0)},
			17: {X: float32(13.0), Y: float32(7.0)},
			18: {X: float32(14.2), Y: float32(7.4)},
			19: {X: float32(15.0), Y: float32(8.3)},
			20: {X: float32(14.6), Y: float32(9.4)},
			21: {X: float32(13.3), Y: float32(9.8)},
			22: {X: float32(12.2), Y: float32(9.1)},
			23: {X: float32(11.8), Y: float32(8.0)},
			24: {X: float32(12.5), Y: float32(7.2)},
			25: {X: float32(4.0), Y: float32(3.5)},
			26: {X: float32(5.0), Y: float32(4.0)},
			27: {X: float32(6.0), Y: float32(4.8)},
			28: {X: float32(7.2), Y: float32(5.1)},
			29: {X: float32(8.5), Y: float32(5.8)},
			30: {X: float32(9.7), Y: float32(6.2)},
			31: {X: float32(10.8), Y: float32(6.8)},
			32: {X: float32(11.5), Y: float32(7.4)},
		},
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
		t.State.String(),
	)

	if t.State == fleetpb.TruckState_IDLE {

		return &fleetpb.Command{
			Type:  fleetpb.CommandType_NEW_ROUTE,
			Route: []int32{rand.Int31n(int32(len(s.world) + 1)), rand.Int31n(int32(len(s.world) + 1))},
		}, nil
	}

	return &fleetpb.Command{
		Type: fleetpb.CommandType_CONTINUE,
	}, nil
}
