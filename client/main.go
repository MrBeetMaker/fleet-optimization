package main

import (
	"context"
	"log"
	"time"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TruckState int

const (
	IDLE TruckState = iota
	DRIVING
	CHARGING
	WAITING
	DELIVERING
)

var StateName = map[TruckState]string{
	IDLE:       "idle",
	DRIVING:    "driving",
	CHARGING:   "charging",
	WAITING:    "waiting",
	DELIVERING: "delivering",
}

type Truck struct {
	id int32
	x  float64
	y  float64

	battery float64

	storageCapacity int
	orders          []int

	destX float64
	destY float64

	State  TruckState
	client fleetpb.FleetServiceClient
}

func (ss TruckState) String() string {
	return StateName[ss]
}

func (t *Truck) SendTelemetry() {

	cmd, err := t.client.SendTelemetry(
		context.Background(),
		&fleetpb.Telemetry{
			TruckId:   t.id,
			X:         t.x,
			Y:         t.y,
			Battery:   t.battery,
			State:     int32(t.State),
			Timestamp: time.Now().Unix(),
		},
	)

	if err != nil {
		log.Println(err)
		return
	}

	t.HandleCommand(cmd)
}

func NewTruck(id int32) *Truck {

	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		log.Fatal(err)
	}

	return &Truck{
		id:      id,
		battery: 100,
		client:  fleetpb.NewFleetServiceClient(conn),
	}
}

func (t *Truck) Register() {

	resp, err := t.client.RegisterTruck(
		context.Background(),
		&fleetpb.RegisterRequest{
			TruckId: t.id,
		},
	)

	if err != nil {
		log.Fatal(err)
	}

	log.Println("Registered:", resp.Accepted)

	for id, point := range resp.Points {
		log.Printf("Point %d: x=%d y=%d", id, point.X, point.Y)
	}
}

func (t *Truck) HandleCommand(cmd *fleetpb.Command) {

	switch cmd.Type {

	case fleetpb.CommandType_STOP:
		t.State = WAITING

	case fleetpb.CommandType_CONTINUE:
		t.State = DRIVING

	case fleetpb.CommandType_NEW_ROUTE:
		log.Println("Received new route:", cmd.Route)
	}
}

func (t *Truck) drive() {

	t.x += 1

	t.y += 0.5

	t.battery -= 0.2
}

func (t *Truck) Run() {

	t.Register()

	ticker := time.NewTicker(time.Second)

	defer ticker.Stop()

	for range ticker.C {
		t.SendTelemetry()

		t.drive()
	}
}

func main() {

	truck := NewTruck(1)

	truck.Run()
}
