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

	current int32 // Current node
	dest    int32
	destX   float64
	destY   float64

	orders  map[int64]*fleetpb.Order
	route   fleetpb.Route
	nodeMap map[int32]*fleetpb.Point

	State  fleetpb.TruckState
	client fleetpb.FleetServiceClient
	conn   *grpc.ClientConn
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
			Current:   t.current,
			Timestamp: time.Now().Unix(),
			Dest:      t.dest,
			Route:     &t.route,
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
		dest:    0,
		current: -1,
		conn:    conn,
		nodeMap: make(map[int32]*fleetpb.Point),
		route:   fleetpb.Route{},
		orders:  make(map[int64]*fleetpb.Order),
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

	log.Printf("Truck %d registered: %t", t.id, resp.Accepted)

	for id, point := range resp.Points {
		log.Printf("Point %d: x=%f y=%f", id, point.X, point.Y)
		t.nodeMap[id] = point
	}
}

func (t *Truck) HandleCommand(cmd *fleetpb.Command) {

	switch cmd.Type {

	case fleetpb.CommandType_STOP:
		t.State = fleetpb.TruckState_WAITING

	case fleetpb.CommandType_CONTINUE:
		if t.State == fleetpb.TruckState_WAITING {
			t.State = fleetpb.TruckState_DRIVING
		}
	case fleetpb.CommandType_NEW_ROUTE:
		log.Printf("Truck %d received new route: %v", t.id, cmd.Route.Nodes)
		t.State = fleetpb.TruckState_DRIVING

		// Append new route
		if cmd.Route != nil {
			t.route.Nodes = append(t.route.Nodes, cmd.Route.Nodes...)
		}

		// Append new orders
		log.Printf("Truck %d received %d new orders: %+v", t.id, len(cmd.Orders), cmd.Orders)
		for i := range cmd.Orders {

			order := cmd.Orders[i]

			if _, alreadyExists := t.orders[order.Id]; alreadyExists {
				log.Printf("WARNING: Order %d already exists in Truck %d. Overwriting old entry.", order.Id, t.id)
			}

			t.orders[order.Id] = order
		}
		if t.dest < 0 && len(t.route.Nodes) > 0 {
			t.setDestination(t.route.Nodes[0])
		}
	}
}

// Marks any order with the current destination as delivered, assuming they have been picked up.
// Should be called when the truck arrives at the current destination.
func (t *Truck) dropOff() {

	node := t.dest

	// Drop all orders with current node as dropOff
	for id, order := range t.orders {
		// If wrong node or order hasn't been picked up yet
		if order.DropOff != node || !order.PickedUp {
			continue
		}

		cmd, err := t.client.RequestDelivery(context.Background(), &fleetpb.DeliverRequest{
			OrderId: id,
			TruckId: t.id,
			NodeId:  node,
		})
		if err != nil {
			log.Fatal(err)
		}

		log.Printf("Truck %d requested delivery of order %d at node %d: %t", t.id, order.Id, node, cmd.Accepted)
		if cmd.Accepted {
			order.Delivered = true
			t.orders[id] = order
		}
	}
}

// Returns true if successful, otherwise false.
func (t *Truck) setDestination(node int32) bool {

	point, exists := t.nodeMap[node]

	if !exists {
		log.Printf("Truck %d failed to set destination: Node %d does not exist.", t.id, node)
		return false
	}

	t.dest = node
	t.destX = float64(point.X)
	t.destY = float64(point.Y)

	return true
}

// Marks any order with the current destination as picked up.
// Should be called right after drop-off.
func (t *Truck) pickUp() {

	node := t.dest

	// Drop all orders with current node as dropOff
	for id, order := range t.orders {
		if order.PickUp != node || order.PickedUp {
			continue
		}

		cmd, err := t.client.RequestPickup(context.Background(), &fleetpb.PickUpRequest{
			OrderId: id,
			TruckId: t.id,
			NodeId:  node,
		})
		if err != nil {
			log.Fatal(err)
		}

		if cmd.Accepted {
			log.Printf("Truck %d requested pick-up of order %d at node %d for delivery at node %d: %t", t.id, order.Id, node, order.DropOff, cmd.Accepted)
			order.PickedUp = true
			t.orders[id] = order
		}
	}
}

// Attempts to set next node in route as destionation, and set state to driving.
// Sets state to idle if route is empty.
func (t *Truck) nextDestination() {

	if len(t.route.Nodes) <= 1 {
		log.Printf("Truck %d has no route (IDLE).", t.id)
		t.State = fleetpb.TruckState_IDLE
		t.dest = -1
		t.route = fleetpb.Route{} // Remove current destination to avoid duplicate visits
		return
	}

	t.route.Nodes = t.route.Nodes[1:] // Pop previous destination

	if t.setDestination(t.route.Nodes[0]) {
		log.Printf("Truck %d is driving to node %d at (%f, %f)", t.id, t.route.Nodes[0], t.destX, t.destY)
		t.State = fleetpb.TruckState_DRIVING
	}
}

func (t *Truck) arrivedAtDestination() bool {

	node := t.dest

	if node < 0 { // Can't arrive at destination if none exists.
		return false
	}

	point, exists := t.nodeMap[node]

	if !exists {
		log.Printf("ERROR: Node %d does not exist in node map.", node)
		return false
	}

	// Debug
	if point.X != float32(t.destX) || point.Y != float32(t.destY) {
		log.Printf("WARNING: Destination does not match route! Destination: (%f, %f), Route: (%f, %f), Truck: (%f, %f). Updating now.", t.destX, t.destY, point.X, point.Y, t.x, t.y)
		t.destX = float64(point.X)
		t.destY = float64(point.Y)
	}

	dx := t.destX - t.x
	dy := t.destY - t.y

	distanceSquared := dx*dx + dy*dy
	arrivalToleranceSquared := 0.01
	departureToleranceSquared := 0.1

	arrived := arrivalToleranceSquared > distanceSquared

	if arrived {
		t.current = node
		log.Printf("Truck %d has arrived at node %d.", t.id, node)

		// Wait a bit before removing current node
	} else if departureToleranceSquared > distanceSquared {
		t.current = -1 // No node
	}
	return arrived
}

func (t *Truck) drive() {

	dx := t.destX - t.x
	dy := t.destY - t.y
	norm := math.Sqrt(dx*dx + dy*dy)

	if norm == 0 {
		return
	}

	speed := 0.1
	if norm < speed { // Avoid overshooting destination
		speed = norm
	}

	dx = dx * speed / norm
	dy = dy * speed / norm

	t.x += dx
	t.y += dy
	t.battery -= speed
}

func (t *Truck) Run() {

	defer t.conn.Close()

	t.Register()

	ticker := time.NewTicker(time.Second / 10)

	defer ticker.Stop()

	for range ticker.C {

		if t.arrivedAtDestination() {
			t.dropOff()
			t.pickUp()
			t.nextDestination()
		} else {
			t.drive()
		}

		t.SendTelemetry()
	}
}

func main() {

	var wg sync.WaitGroup

	nrOfTrucks := 1
	for i := range nrOfTrucks {
		wg.Add(1)
		go func(i int32) {
			// defer wg.Done()
			NewTruck(i).Run()
		}(int32(i))
	}

	wg.Wait()

}
