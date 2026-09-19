package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
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

	// postgreSQL Database connection
	db *Database

	// Maps truck id's to a list of commands
	commands map[int32][]*fleetpb.Command
}

// Help function for converting database records of trucks to proto structs
func toProtoTruckState(state TruckState) fleetpb.TruckState {
	switch state {
	case TruckIdle:
		return fleetpb.TruckState_IDLE
	case TruckDriving:
		return fleetpb.TruckState_DRIVING
	case TruckCharging:
		return fleetpb.TruckState_CHARGING
	case TruckWaiting:
		return fleetpb.TruckState_WAITING
	case TruckOffline:
		return fleetpb.TruckState_OFFLINE
	case TruckDelivering:
		return fleetpb.TruckState_DELIVERING
	default:
		return fleetpb.TruckState_IDLE
	}
}

// Help function for converting proto truck states to database records
func toDatabaseTruckState(state fleetpb.TruckState) TruckState {
	switch state {
	case fleetpb.TruckState_IDLE:
		return TruckIdle
	case fleetpb.TruckState_DRIVING:
		return TruckDriving
	case fleetpb.TruckState_CHARGING:
		return TruckCharging
	case fleetpb.TruckState_WAITING:
		return TruckWaiting
	case fleetpb.TruckState_OFFLINE:
		return TruckOffline
	case fleetpb.TruckState_DELIVERING:
		return TruckDelivering
	default:
		return TruckIdle
	}
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

func NewFleetServer(db *Database, optimizer fleetpb.OptimizerServiceClient) (*FleetServer, error) {

	ctx := context.Background()

	// Retrieve world map
	dbWorld, err := db.GetPoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("Load points: %w", err)
	}

	// Initiate truck registry
	dbTrucks, err := db.GetTrucks(ctx)
	if err != nil {
		return nil, fmt.Errorf("Load trucks: %w", err)
	}

	// Retrieve orders
	dbOrders, err := db.GetActiveOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("Load orders: %w", err)
	}

	// Init commands
	commands := make(map[int32][]*fleetpb.Command)

	// Convert database records to protobuf/domain representations.

	nodeMap := make(map[int32]*fleetpb.Point, len(dbWorld))
	for _, p := range dbWorld {
		nodeMap[p.ID] = &fleetpb.Point{
			X: float32(p.X),
			Y: float32(p.Y),
		}
	}

	trucks := make(map[int32]*TruckInfo, len(dbTrucks))
	for _, t := range dbTrucks {
		trucks[t.ID] = &TruckInfo{
			Battery: 0,
			State:   toProtoTruckState(t.State),
			X:       t.X,
			Y:       t.Y,
			Dest:    -1,
			Current: -1,
			Route:   fleetpb.Route{},
		}
	}

	orders := make(map[int64]*fleetpb.Order, len(dbOrders))
	for _, o := range dbOrders {
		order := &fleetpb.Order{
			Id:        o.ID,
			Weight:    float32(o.Weight),
			Size:      o.Size,
			PickUp:    o.PickupID,
			DropOff:   o.DropoffID,
			PickedUp:  o.State == OrderPickedUp || o.State == OrderDelivered,
			Delivered: o.State == OrderDelivered,
		}

		// proto3 has no nullable scalar, so use -1 when TruckID is nil.
		order.TruckId = -1
		if o.TruckID != nil {
			order.TruckId = *o.TruckID
		}

		orders[o.ID] = order
	}

	log.Printf("\t* Retrieved %d nodes.", len(nodeMap))
	log.Printf("\t* Retrieved %d trucks", len(trucks))
	log.Printf("\t* Retrieved %d orders", len(orders))

	return &FleetServer{
		db:        db,
		optimizer: optimizer,
		world:     nodeMap,
		trucks:    trucks,
		orders:    orders,
		commands:  commands,
	}, nil
}

// Sends the nodeMap to the Optimizer
func (s *FleetServer) InitOptimizer() (bool, error) {
	maxRetries := 5
	backoffDuration := time.Second * 2

	resp, err := retry(func() (*fleetpb.InitResponse, error) {

		resp, err := s.optimizer.InitializeOptimizer(
			context.Background(),
			&fleetpb.InitRequest{
				Points: s.world,
			},
		)

		if err != nil {
			return &fleetpb.InitResponse{
				Success: false}, err
		} else {
			return resp, err
		}

	}, maxRetries, backoffDuration)

	return resp.Success, err
}

// Retrieves and inserts pending orders from the database.
func (s *FleetServer) GetPendingOrders() error {

	s.ordersMutex.Lock()

	// Retrieve orders
	dbOrders, err := s.db.GetPendingOrders(context.Background())
	if err != nil {
		return fmt.Errorf("ERROR: Failed to load orders: %v", err)
	}

	orders := make(map[int64]*fleetpb.Order, len(dbOrders))
	for _, o := range dbOrders {

		if o.State != OrderPending {
			log.Printf("WARNING: Order %d was marked as %s in the database, but returned when retrieving pending orders.", o.ID, o.State)
			continue
		}

		// Reject orders with unknown locations; the server should retrieve the new nodes.
		if _, dropOffExists := s.world[o.DropoffID]; !dropOffExists {
			log.Printf("WARNING: Order %d has unknown drop off: %d.", o.ID, o.DropoffID)
			continue
		}
		if _, pickUpExists := s.world[o.PickupID]; !pickUpExists {
			log.Printf("WARNING: Order %d has unknown pick-up: %d.", o.ID, o.PickupID)
			continue
		}

		order := &fleetpb.Order{
			Id:        o.ID,
			Weight:    float32(o.Weight),
			Size:      o.Size,
			PickUp:    o.PickupID,
			DropOff:   o.DropoffID,
			PickedUp:  o.State == OrderPickedUp || o.State == OrderDelivered,
			Delivered: o.State == OrderDelivered,
		}

		// Use -1 when TruckID is nil.
		order.TruckId = -1
		if o.TruckID != nil {
			order.TruckId = *o.TruckID
		}

		// If the order has already been retrieved, then we don't want to overwrite it.
		// The server is responsible for holding the current state, and send updates to the database.
		if _, exists := s.orders[o.ID]; !exists {
			orders[o.ID] = order
		} else {
			log.Printf("Order %d already exists in orders.", o.ID)
		}
	}

	s.ordersMutex.Unlock()

	return nil

}

func (s *FleetServer) RunPeriodicRoutePlanning() {

	success, err := s.InitOptimizer()

	if err != nil || !success {
		log.Panic("Optimizer could not recieve node map!")
	}

	s.createRoutes()

	interval := 20 * time.Second

	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for range ticker.C {

		s.GetPendingOrders()

		s.createRoutes()
	}
}

// Send a request for route creation to the optimizer service.
// Aborts if no trucks are available
func (s *FleetServer) createRoutes() {
	maxWaitDuration := 45 * time.Second

	s.ordersMutex.RLock()
	total := len(s.orders)
	orders := make([]*fleetpb.Order, 0, total)
	for _, order := range s.orders {
		// Skip orders that are already been assigned
		if order.TruckId >= 0 {
			continue
		}

		// Deep copy to avoid data race
		orders = append(orders, proto.Clone(order).(*fleetpb.Order))
	}

	s.ordersMutex.RUnlock()

	log.Printf("Found %d pending orders...", len(orders))
	if len(orders) == 0 {
		return
	}

	found := 0
	nrOfTrucks := int32(0)
	s.truckMutex.RLock()
	for truckId := range s.trucks {
		truck, exists := s.trucks[truckId]
		if exists {
			found += 1
		}
		if truck.State != fleetpb.TruckState_OFFLINE {
			nrOfTrucks += 1
		}
	}
	s.truckMutex.RUnlock()

	if nrOfTrucks == 0 {
		log.Printf("Can't create routes: found %d trucks, but %d are online.", found, nrOfTrucks)
		return
	}
	log.Printf("Found %d available trucks.", nrOfTrucks)

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

	log.Printf("Optimizer returned routes:")
	for i, route := range resp.Routes {
		log.Printf("\tRoute %d: %v", i, route.Nodes)
	}

	s.assignRoutes(resp.Routes, resp.OrderIds)
}

// Assigns routes to trucks
// Currently excludes trucks that are offline
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

	// Make sure to exclude trucks that are offline
	for truckId, truck := range s.trucks {
		if truck.State == fleetpb.TruckState_OFFLINE {
			truckDone[truckId] = true
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

		log.Printf("Assigned route %d to Truck %d: lowest distance to first node %.2f", routeId, bestTruckId, bestDistance)
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

				err := s.db.AssignOrder(context.Background(), orderId, order.TruckId)
				if err != nil {
					log.Printf("ERROR: Could not update database with order %d assigned to truck %d: %v", orderId, order.TruckId, err)
				}
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

	if accepted {
		err := s.db.MarkOrderPickedUp(ctx, req.OrderId, order.TruckId)
		if err != nil {
			log.Printf("ERROR: Could not mark order %d as picked up by truck %d on the database: %v", req.OrderId, req.TruckId, err)
		}
	}

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

	if accepted {
		err := s.db.MarkOrderDelivered(ctx, req.OrderId, order.TruckId)
		if err != nil {
			log.Printf("ERROR: Could not mark order %d as picked up by truck %d on the database: %v", req.OrderId, req.TruckId, err)
		}
	}

	return &fleetpb.DeliverResponse{
		Accepted: accepted,
	}, nil
}

func (s *FleetServer) RegisterTruck(ctx context.Context, req *fleetpb.RegisterRequest) (*fleetpb.RegisterResponse, error) {

	s.truckMutex.Lock()
	defer s.truckMutex.Unlock()

	if truck, exists := s.trucks[req.TruckId]; exists && truck.State != fleetpb.TruckState_OFFLINE {
		log.Printf("Declined registration request for Truck %d: Truck %d is already registered and %s.", req.TruckId, req.TruckId, truck.State)

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

	// Update database. In the future, this may be moved to a que and handled separately
	if truckExists {
		err := s.db.UpdateTruck(ctx, t.TruckId, toDatabaseTruckState(t.State), t.X, t.Y)

		if err != nil {
			log.Printf("ERROR: Could not update Truck %d in database: %v", t.TruckId, err)
		}
	}

	if command := s.popCommand(t.TruckId); truckExists && command != nil {
		return command, nil
	}

	return &fleetpb.Command{
		Type: fleetpb.CommandType_CONTINUE,
	}, nil
}
