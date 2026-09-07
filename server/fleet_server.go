package main

import (
	"context"
	"errors"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
)

type routeAssignment struct {
	truckId int32
	routeId int32
	route   *fleetpb.Route
}

type FleetServer struct {
	fleetpb.UnimplementedFleetServiceServer

	// If you need multiple mutexes, lock them in this order:
	//
	// (1): truckMutex (2): ordersMutex (3): commandsMutex
	truckMutex sync.RWMutex
	// If you need multiple mutexes, lock them in this order:
	//
	// (1): truckMutex (2): ordersMutex (3): commandsMutex
	ordersMutex sync.RWMutex
	// If you need multiple mutexes, lock them in this order:
	//
	// (1): truckMutex (2): ordersMutex (3): commandsMutex
	commandsMutex sync.RWMutex

	// Maps node id's to Points with x and y coordinates. Immutable for the time being.
	world map[int32]*fleetpb.Point
	// Holds information on each truck.
	trucks map[int32]*TruckInfo
	// Maps order id's to their order info
	orders map[int64]*fleetpb.Order

	// Schedules and optimizes routes
	optimizer fleetpb.OptimizerServiceClient

	// Maps truck id's to a list of commands
	commands map[int32][]*fleetpb.Command
}

type TruckInfo struct {
	Battery float64
	State   fleetpb.TruckState
	X       float64
	Y       float64
	Dest    int32
	Current int32
	Route   fleetpb.Route
}

func NewFleetServer(optimizer fleetpb.OptimizerServiceClient) *FleetServer {

	// Retrieve world map
	nodeMap := retrieveNodeMap()

	// Initiate truck registry
	trucks := retrieveTrucks()

	// Retrieve orders
	orders := retrieveOrders()

	// Init commands
	commands := make(map[int32][]*fleetpb.Command)

	return &FleetServer{
		optimizer: optimizer,
		trucks:    trucks,
		orders:    orders,
		world:     nodeMap,
		commands:  commands,
	}
}

func (s *FleetServer) RunPeriodicRoutePlanning() {

	s.createRoutes()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.createRoutes()
	}
}

// Send a request for route creation to the optimizer service.
func (s *FleetServer) createRoutes() {

	s.ordersMutex.RLock()
	orders := make([]*fleetpb.Order, 0, len(s.orders))
	for _, order := range s.orders {
		// Deep copy to avoid data race
		orders = append(orders, proto.Clone(order).(*fleetpb.Order))
	}
	s.ordersMutex.RUnlock()

	if len(orders) == 0 {
		return
	}

	s.truckMutex.RLock()
	nrOfTrucks := int32(len(s.trucks))
	s.truckMutex.RUnlock()

	if nrOfTrucks == 0 {
		return
	}

	maxWaitDuration := 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitDuration)
	defer cancel()

	resp, err := s.optimizer.CreateRoute(
		ctx,
		&fleetpb.CreateRouteRequest{
			NrOfTrucks: nrOfTrucks,
			Orders:     orders,
			Points:     s.world,
		},
	)

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			log.Printf("Optimizer timed out after %s: %v", maxWaitDuration, err)
		} else {
			log.Printf("Optimizer error: %v", err)
		}
		return
	}

	log.Printf("Optimizer returned routes: %v", resp.Routes)

	s.assignRoutes(resp.Routes, resp.OrderIds)
}

// Assigns routes to trucks
func (s *FleetServer) assignRoutes(routes map[int32]*fleetpb.Route, orderIds map[int32]*fleetpb.OrderIds) {
	s.truckMutex.Lock()

	routeDone := make(map[int32]bool, len(routes))
	truckDone := make(map[int32]bool, len(s.trucks))

	assignments := []routeAssignment{}

	// Exclude any empty routes, or routes with invalid nodes
	for routeId, route := range routes {
		if len(route.Nodes) == 0 {
			routeDone[routeId] = true
			continue
		}
		if _, nodeExists := s.world[route.Nodes[0]]; !nodeExists {
			routeDone[routeId] = true
			log.Printf("WARNING: Route %d contains node %d: Node does not exist in node map!", routeId, route.Nodes[0])
		}
	}

	// First assign routes that connect directly to trucks.
	for truckId, truck := range s.trucks {
		if truckDone[truckId] {
			continue
		}

		for routeId, route := range routes {
			if routeDone[routeId] {
				continue
			}

			routeStart := route.Nodes[0]
			truckCurrent := truck.Current
			truckEnd := truck.Dest

			if truck.State == fleetpb.TruckState_IDLE && truckCurrent == routeStart {

				truckDone[truckId] = true
				routeDone[routeId] = true

				assignments = append(assignments, routeAssignment{truckId: truckId, route: route, routeId: routeId})

				log.Printf("Assigned route %d to Truck %d: same start node %d", routeId, truckId, routeStart)
				break
			}

			if truck.State == fleetpb.TruckState_DRIVING && truckEnd == routeStart {

				truckDone[truckId] = true
				routeDone[routeId] = true

				assignments = append(assignments, routeAssignment{truckId: truckId, route: route, routeId: routeId})

				log.Printf("Assigned route %d to Truck %d: routes connect at node %d", routeId, truckId, routeStart)
				break
			}
		}
	}

	// Assign any remaining routes to the closest available truck.
	for routeId, route := range routes {
		if routeDone[routeId] {
			continue
		}

		routeStart := route.Nodes[0]

		var bestTruckId int32
		bestDistance := math.MaxFloat64
		foundTruck := false

		// Calculate distance between truck and route start
		for truckId, truck := range s.trucks {
			if truckDone[truckId] {
				continue
			}

			var truckX float32
			var truckY float32

			// Use truck destination if possible
			if truckPoint, exists := s.world[truck.Dest]; exists {
				truckX = truckPoint.X
				truckY = truckPoint.Y

				// Otherwise use truck position
			} else {
				truckX = float32(truck.X)
				truckY = float32(truck.Y)
			}

			// Note: any nodes without an entry in s.world are marked as done at the
			// beginning of this function and are therefore skipped before reaching this point.
			node := s.world[routeStart]
			nodeX := node.X
			nodeY := node.Y

			dx := truckX - nodeX
			dy := truckY - nodeY
			distance := math.Sqrt(float64(dx*dx + dy*dy))

			if distance < bestDistance {
				bestDistance = distance
				bestTruckId = truckId
				foundTruck = true
			}
		}

		if !foundTruck {
			continue
		}

		truckDone[bestTruckId] = true
		routeDone[routeId] = true

		assignments = append(assignments, routeAssignment{truckId: bestTruckId, route: route, routeId: routeId})

		log.Printf("Assigned route %d to Truck %d: distance %.2f", routeId, bestTruckId, bestDistance)
	}

	s.truckMutex.Unlock()

	for routeId := range routes {

		if routeDone[routeId] {
			continue
		}
		log.Printf("WARNING: Could not assign route %d to any truck!", routeId)
	}

	for _, assignment := range assignments {
		orderIds := orderIds[assignment.routeId]
		orders := make([]*fleetpb.Order, 0, len(orderIds.Ids))

		s.ordersMutex.Lock()
		for _, orderId := range orderIds.Ids {
			if order, exists := s.orders[orderId]; exists {
				order.TruckId = assignment.truckId
				orders = append(orders, order)
			}
		}
		s.ordersMutex.Unlock()

		s.addCommand(assignment.truckId, &fleetpb.Command{
			Type:   fleetpb.CommandType_NEW_ROUTE,
			Route:  assignment.route,
			Orders: orders,
		})
	}
}

func (s *FleetServer) addCommand(truckId int32, command *fleetpb.Command) {

	s.commandsMutex.Lock()
	defer s.commandsMutex.Unlock()

	queue := s.commands[truckId]

	// Merge consecutive NEW_ROUTE commands into one.
	if command.Type == fleetpb.CommandType_NEW_ROUTE && len(queue) > 0 && queue[len(queue)-1].Type == fleetpb.CommandType_NEW_ROUTE {

		last := queue[len(queue)-1]
		last.Route.Nodes = append(last.Route.Nodes, command.Route.Nodes...)
		last.Orders = append(last.Orders, command.Orders...)

		return
	}

	s.commands[truckId] = append(queue, command)
}

func (s *FleetServer) popCommand(truckId int32) *fleetpb.Command {
	s.commandsMutex.Lock()
	defer s.commandsMutex.Unlock()

	queue := s.commands[truckId]

	if len(queue) == 0 {
		return nil
	}

	command := queue[0]
	s.commands[truckId] = queue[1:]

	if len(s.commands[truckId]) == 0 {
		delete(s.commands, truckId)
	}

	return command
}

// Accepts pick-up if order is at the specified node. Also validates node, order and truck.
func (s *FleetServer) RequestPickup(ctx context.Context, req *fleetpb.PickUpRequest) (*fleetpb.PickUpResponse, error) {

	_, nodeExists := s.world[req.NodeId]

	s.ordersMutex.RLock()
	order, orderExists := s.orders[req.OrderId]
	orderExistsAtNode := orderExists && order.PickUp == req.NodeId
	s.ordersMutex.RUnlock()

	s.truckMutex.RLock()
	_, truckExists := s.trucks[req.TruckId] // ToDo: assign orders to specific truck, and decline pickup by other trucks
	s.truckMutex.RUnlock()

	accepted := nodeExists && orderExists && truckExists && orderExistsAtNode

	log.Printf("Truck %d requested pick-up of order %d at node %d: %t", req.TruckId, req.OrderId, req.NodeId, accepted)

	return &fleetpb.PickUpResponse{
		Accepted: accepted,
	}, nil
}

// Accepts delivery if order is supposed to be delivered at node. Also validates node, order and truck.
func (s *FleetServer) RequestDelivery(ctx context.Context, req *fleetpb.DeliverRequest) (*fleetpb.DeliverResponse, error) {
	_, nodeExists := s.world[req.NodeId]

	s.ordersMutex.RLock()
	order, orderExists := s.orders[req.OrderId]
	dropOffIsAtNode := orderExists && order.DropOff == req.NodeId
	s.ordersMutex.RUnlock()

	s.truckMutex.RLock()
	_, truckExists := s.trucks[req.TruckId]
	s.truckMutex.RUnlock()

	accepted := nodeExists && orderExists && truckExists && dropOffIsAtNode

	log.Printf("Truck %d requested delivery of order %d at node %d: %t", req.TruckId, req.OrderId, req.NodeId, accepted)

	return &fleetpb.DeliverResponse{
		Accepted: accepted,
	}, nil
}

func (s *FleetServer) RegisterTruck(ctx context.Context, req *fleetpb.RegisterRequest) (*fleetpb.RegisterResponse, error) {

	s.truckMutex.Lock()
	defer s.truckMutex.Unlock()

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
	s.truckMutex.Lock()

	truck, truckExists := s.trucks[t.TruckId]

	if truckExists {
		truck.X = t.X
		truck.Y = t.Y
		truck.State = t.State
		truck.Battery = t.Battery
		truck.Dest = t.Dest
		truck.Current = t.Current

		if t.Route != nil {
			truck.Route = *t.Route
		}

		log.Printf("Truck %d (%.1f, %.1f) Battery %.1f (%s) Destination %d", t.TruckId, t.X, t.Y, t.Battery, t.State.String(), t.Dest)
	}

	s.truckMutex.Unlock()

	if command := s.popCommand(t.TruckId); truckExists && command != nil {
		return command, nil
	}

	return &fleetpb.Command{
		Type: fleetpb.CommandType_CONTINUE,
	}, nil
}

// ToDo: implement SQL database and retrieve nodes from there.
func retrieveNodeMap() map[int32]*fleetpb.Point {
	return map[int32]*fleetpb.Point{
		0:  {X: float32(0.0), Y: float32(0.0)},
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
	}
}

// ToDo: implement SQL database and retrieve trucks from there.
func retrieveTrucks() map[int32]*TruckInfo {
	return map[int32]*TruckInfo{}
}

// ToDo: implement SQL database and retrieve orders from there.
func retrieveOrders() map[int64]*fleetpb.Order {

	nrOfOrders := 10

	orders := make(map[int64]*fleetpb.Order)

	for range nrOfOrders {

		orderId := rand.Int63n(999999)
		pickUp := rand.Int31n(17)
		dropOff := rand.Int31n(16) + 17
		size := 1 + rand.Int31n(9)
		weight := float32(size * int32(1+10*rand.Float32()))
		truckId := int32(-1) // No truck assigned

		orders[orderId] = &fleetpb.Order{
			Id:        orderId,
			PickUp:    pickUp,
			DropOff:   dropOff,
			Size:      size,
			Weight:    weight,
			PickedUp:  false,
			Delivered: false,
			TruckId:   truckId,
		}
	}

	return orders
}
