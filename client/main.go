package main

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Truck struct {
	id int32
	x  float64
	y  float64

	battery float64

	storageCapacity int
	orders          []int

	destX float64
	destY float64

	route   []int32 // Nods id's
	nodeMap map[int32]*fleetpb.Point

	State  fleetpb.TruckState
	client fleetpb.FleetServiceClient
}

func (t *Truck) SendTelemetry() {

	cmd, err := t.client.SendTelemetry(
		context.Background(),
		&fleetpb.Telemetry{
			TruckId:   t.id,
			X:         t.x,
			Y:         t.y,
			Battery:   t.battery,
			State:     t.State,
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
		x:       0,
		y:       0,
		destX:   0,
		destY:   0,
		nodeMap: make(map[int32]*fleetpb.Point),
		route:   make([]int32, 0),
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
		// log.Printf("Point %d: x=%f y=%f", id, point.X, point.Y)
		t.nodeMap[id] = point
	}
}

func (t *Truck) HandleCommand(cmd *fleetpb.Command) {

	switch cmd.Type {

	case fleetpb.CommandType_STOP:
		t.State = fleetpb.TruckState_WAITING

	case fleetpb.CommandType_CONTINUE:
		t.State = fleetpb.TruckState_DRIVING

	case fleetpb.CommandType_NEW_ROUTE:
		log.Println("Received new route:", cmd.Route)
		t.State = fleetpb.TruckState_DRIVING

		// Append new route
		for i := range cmd.Route {
			t.route = append(t.route, cmd.Route[int32(i)])
		}
	}
}

// Attempts to set next node in route as destionation, and set state to driving.
// Sets state to idle if route is empty.
func (t *Truck) nextDestination() {

	if len(t.route) <= 1 {
		log.Printf("Truck %d has no route (IDLE).", t.id)
		t.State = fleetpb.TruckState_IDLE
		return
	}

	t.route = t.route[1:] // Pop previous destination

	nodeId := t.route[0]
	dest := t.nodeMap[nodeId]

	t.destX = float64(dest.X)
	t.destY = float64(dest.Y)

	t.State = fleetpb.TruckState_DRIVING
	log.Printf("Truck %d is driving to node %d at (%f, %f)", t.id, nodeId, t.destX, t.destY)
}

func (t *Truck) drive() {

	dx := t.destX - t.x
	dy := t.destY - t.y

	norm := math.Sqrt(dx*dx + dy*dy)

	speed := 0.1

	if norm < speed || norm == 0 { // Arrived at destination
		log.Printf("Truck %d at (%f, %f) has arrived at destination (%f, %f)", t.id, t.x, t.y, t.destX, t.destY)
		t.State = fleetpb.TruckState_WAITING

		time.Sleep(time.Second)

		t.nextDestination()

		// Print both truck position and destination for now.

		return
	}

	dx = dx * speed / norm
	dy = dy * speed / norm

	t.x += dx
	t.y += dy

	t.battery -= speed * float64(1+len(t.orders)) // Placeholder battery cost

}

func (t *Truck) Run() {

	t.Register()

	ticker := time.NewTicker(time.Second / 10)

	defer ticker.Stop()

	for range ticker.C {
		t.SendTelemetry()

		t.drive()
	}
}

func main() {

	var wg sync.WaitGroup

	nrOfTrucks := 3
	for i := range nrOfTrucks {
		go NewTruck(int32(i)).Run()
		wg.Add(1)
	}

	wg.Wait()

}
