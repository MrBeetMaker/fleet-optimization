from concurrent import futures
from time import perf_counter
import math
import grpc
from proto import fleet_pb2
from proto import optim_pb2
from proto import optim_pb2_grpc
from ortools.constraint_solver import pywrapcp
from ortools.constraint_solver import routing_enums_pb2

class OptimizerService(optim_pb2_grpc.OptimizerServiceServicer):

    def __init__(self):
        self.points = dict()
        self.scale = 1000           # Use for scaling node distances

    def euclidean_distance_scaled_int(self, a, b) -> int:
        """Returns scaled up integer distance.

        OR-Tools needs integer distances, so we multiply by self.scale
        in order to keep a number of decimal places, and make sure it's an integer."""

        return round(math.hypot(a[0] - b[0], a[1] - b[1]) * self.scale)

    def InitializeOptimizer(self, request, context):

        print(f"Optimizer received {len(request.points)} nodes:")

        self.points = dict()
        for point, coords in request.points.items():
            self.points[point] = (coords.x, coords.y)
            print(self.points[point])

        return optim_pb2.InitResponse(
            success=True,
        )

    def validate_order(self, order):
        """Returns true if order is valid, otherwise false."""

        print(
            "id:", order.id,
            "pickUp:", order.pickUp,
            "dropOff:", order.dropOff,
            "size:", order.size,
            end=""
        )

        valid_pickup = order.pickUp in self.points
        valid_dropoff = order.dropOff in self.points

        if not valid_pickup:
            print(f"\t\tINVALID! Order {order.id} has invalid pickup node {order.pickUp}")
            return False
        if not valid_dropoff:
            print(f"\t\tINVALID! Order {order.id} has invalid dropoff node {order.dropOff}")
            return False

        print("\t\tVALID")

        return True

    def CreateRoute(self, request, context):
        start = perf_counter()

        nr_of_trucks = request.nrOfTrucks
        orders_included = list()
        compressed_indx_to_node = dict()
        nodes = list()
        precedence_pairs = list()

        # Add depot node
        compressed_indx_to_node[0] = 0
        nodes.append(self.points[0])

        print("Validating orders and collecting relevant nodes:")
        for order in request.orders:
            if self.validate_order(order):
                pickup_index = len(nodes)
                nodes.append(self.points[order.pickUp])
                compressed_indx_to_node[pickup_index] = order.pickUp

                dropoff_index = len(nodes)
                nodes.append(self.points[order.dropOff])
                compressed_indx_to_node[dropoff_index] = order.dropOff

                precedence_pairs.append((pickup_index, dropoff_index))
                orders_included.append(order.id)

        if len(orders_included) == 0:
            print("No orders included in route. Returning")

            return optim_pb2.CreateRouteResponse(
                routes={0: fleet_pb2.Route(nodes=[])},
                order_ids={0: optim_pb2.OrderIds(ids=[])}
            )

        print(f"Including {len(orders_included)} orders in route planning after {(perf_counter() - start):2f} seconds.")
        print(f"Nodes: {len(nodes)}")
        print(f"Precedence_pairs: {precedence_pairs}")
        print(f"Number of trucks: {nr_of_trucks}")

        # Setup OR-tools
        manager = pywrapcp.RoutingIndexManager(
            len(nodes),
            nr_of_trucks,
            0,
        )

        routing = pywrapcp.RoutingModel(manager)

        # Precalculate distances
        distance_matrix = [[self.euclidean_distance_scaled_int(a, b) for b in nodes] for a in nodes]

        # Register callback function for travel costs
        def distance_callback(node_a: int, node_b: int):
            indx_a = manager.IndexToNode(node_a)
            indx_b = manager.IndexToNode(node_b)
            return distance_matrix[indx_a][indx_b]

        transit_callback_index = routing.RegisterTransitCallback(distance_callback)

        # Minimize total route distance.
        routing.SetArcCostEvaluatorOfAllVehicles(transit_callback_index)

        # Maximum distance for trucks.
        max_route_distance = 90 * self.scale

        slack = 10

        # Add a dimension whose cumulative value represents distance traveled
        # from the start of a route.
        routing.AddDimension(
            transit_callback_index,
            slack,
            max_route_distance,     # maximum route distance
            True,                   # Route distances start at zero
            "Distance",
        )

        distance_dimension = routing.GetDimensionOrDie("Distance")
        magic_number = 14
        distance_dimension.SetGlobalSpanCostCoefficient(magic_number)

        # Add precedence constraints.
        for pickup_node, dropoff_node in precedence_pairs:
            pickup_index = manager.NodeToIndex(pickup_node)
            dropoff_index = manager.NodeToIndex(dropoff_node)

            # print(f"PD: nodes {pickup_node}->{dropoff_node},\nindices {pickup_index}->{dropoff_index}")

            routing.AddPickupAndDelivery(pickup_index, dropoff_index)

            # Both nodes must use the same vehicle.
            routing.solver().Add(routing.VehicleVar(pickup_index) == routing.VehicleVar(dropoff_index))

            # The vehicle must reach node a before node b
            routing.solver().Add(distance_dimension.CumulVar(pickup_index) <= distance_dimension.CumulVar(dropoff_index))

        print(f"Preparing for route search took {(perf_counter() - start):2f} seconds.")

        print("Starting route search... ", end="", flush=True)

        # Search configuration.
        search_parameters = pywrapcp.DefaultRoutingSearchParameters()
        search_parameters.first_solution_strategy = (
            routing_enums_pb2.FirstSolutionStrategy.PARALLEL_CHEAPEST_INSERTION
        )
        search_parameters.local_search_metaheuristic = (
            routing_enums_pb2.LocalSearchMetaheuristic.AUTOMATIC
        )

        search_parameters.time_limit.seconds = 18       # Max total time to find solution
        search_parameters.lns_time_limit.seconds = 3    # Max time to spend on optimizing locally
        # search_parameters.solution_limit = 10000        # Stop after finding this many solutions. Use for debugging
        search_parameters.use_full_propagation = False  

        solution = routing.SolveWithParameters(search_parameters)

        print(f"Done in {(perf_counter() - start):2f} seconds.")

        if solution:
            routes = dict()
            order_ids = {vehicle_id: [] for vehicle_id in range(nr_of_trucks)}

            # Get the orders for each route
            for i, (pickup_node, _) in enumerate(precedence_pairs):
                vehicle = solution.Value(routing.VehicleVar(manager.NodeToIndex(pickup_node)))
                order_ids[vehicle].append(orders_included[i])

            # Get the actual routes
            for vehicle_id in range(nr_of_trucks):
                index = routing.Start(vehicle_id)
                route = list()

                while not routing.IsEnd(index):
                    node_id = compressed_indx_to_node[manager.IndexToNode(index)]
                    route.append(node_id)
                    index = solution.Value(routing.NextVar(index))

                route.append(compressed_indx_to_node[manager.IndexToNode(index)])

                # Remove consecutive duplicates
                route = [node for i, node in enumerate(route) if i == 0 or node != route[i - 1]]

                routes.update({vehicle_id: fleet_pb2.Route(nodes=route)})
                print(f"Vehicle {vehicle_id}: {route}")

            print(f"Total distance: {solution.ObjectiveValue() / 1000}", flush=True)

            duration = perf_counter() - start
            print(f"Finished in {duration:.2f} seconds.")

            return optim_pb2.CreateRouteResponse(
                routes=routes,
                order_ids={
                    vehicle_id: optim_pb2.OrderIds(ids=ids)
                    for vehicle_id, ids in order_ids.items()
                }
            )
        else:
            print("No solution found.")

            duration = perf_counter() - start
            print(f"Finished in {duration:.2f} seconds.", flush=True)

            return optim_pb2.CreateRouteResponse(
                routes={0: fleet_pb2.Route(nodes=[])},
                order_ids={0: optim_pb2.OrderIds(ids=[])}
            )

def main():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    optim_pb2_grpc.add_OptimizerServiceServicer_to_server(
        OptimizerService(),
        server,
    )

    server.add_insecure_port("[::]:50052")
    server.start()

    print("Optimizer listening on :50052")

    server.wait_for_termination()

def test_optimizer_service():
    """
    Runs OptimizerService locally without starting a gRPC server.
    """

    optimizer_service = OptimizerService()

    # Initialize optimizer

    coordinates = {
        0: (0.0, 0.0),
        1: (1.0, 1.0),
        2: (1.8, 1.4),
        3: (2.5, 2.1),
        4: (1.6, 2.6),
        5: (0.8, 2.0),
        6: (2.1, 0.7),
        7: (3.0, 1.2),
        8: (3.4, 2.3),
        9: (7.0, 1.0),
        10: (8.2, 1.3),
        11: (8.8, 2.2),
        12: (8.1, 3.0),
        13: (6.9, 3.2),
        14: (6.1, 2.3),
        15: (6.2, 1.4),
        16: (7.5, 2.0),
        17: (13.0, 7.0),
        18: (14.2, 7.4),
        19: (15.0, 8.3),
        20: (14.6, 9.4),
        21: (13.3, 9.8),
        22: (12.2, 9.1),
        23: (11.8, 8.0),
        24: (12.5, 7.2),
        25: (4.0, 3.5),
        26: (5.0, 4.0),
        27: (6.0, 4.8),
        28: (7.2, 5.1),
        29: (8.5, 5.8),
        30: (9.7, 6.2),
        31: (10.8, 6.8),
        32: (11.5, 7.4),
    }

    init_request = optim_pb2.InitRequest()

    for point_id, (x, y) in coordinates.items():
        init_request.points[point_id].x = x
        init_request.points[point_id].y = y

    init_response = optimizer_service.InitializeOptimizer(
        init_request,
        None,  # gRPC context is not needed for a local test
    )

    print("InitializeOptimizer response:")
    print(init_response)

    if not init_response.success:
        raise RuntimeError("Optimizer initialization failed")

    # Create route request.

    order_data = [
        # id, weight, size, pickup, dropoff
        (1014, 18, 4, 20, 22),
        (1010, 15, 2, 3, 9),
        (1012, 12, 4, 3, 31),
        (1004, 63.5, 7, 4, 20),
        (1006, 77, 8, 6, 22),
        (1007, 7.5, 1, 7, 23),
        (1009, 5, 4, 9, 16),
        (1001, 18.5, 3, 1, 17),
        (1002, 42, 5, 2, 18),
        (1005, 31, 4, 5, 21),
        (1011, 42, 8, 5, 22),
        (1003, 11, 2, 3, 19),
        (1013, 1, 5, 1, 2),
        (1015, 8, 1, 3, 10),
        (1008, 52, 6, 8, 24),
    ]

    create_route_request = optim_pb2.CreateRouteRequest(
        nrOfTrucks=10,
    )

    for point_id, (x, y) in coordinates.items():
        create_route_request.points[point_id].x = x
        create_route_request.points[point_id].y = y

    for order_id, weight, size, pickup, dropoff in order_data:
        order = create_route_request.orders.add()
        order.id = order_id
        order.weight = weight
        order.size = size
        order.pickUp = pickup
        order.dropOff = dropoff
        order.truckId = -1

    response = optimizer_service.CreateRoute(
        create_route_request,
        None,  # gRPC context is not needed for a local test
    )

    print("\nCreateRoute response:")
    print(response)

    print("\nRoutes:")
    for route_id, route in response.routes.items():
        print(f"Route {route_id}: {list(route.nodes)}")

    print("\nOrders per route:")
    for route_id, order_ids in response.order_ids.items():
        print(f"Route {route_id}: {list(order_ids.ids)}")

        
    print("\nDistance by route:")
    total_distance = 0
    for vehicle_id, route_message in response.routes.items():
        route = list(route_message.nodes)
        route_distance = 0
        for from_id, to_id in zip(route, route[1:]):
            from_point = coordinates[from_id]
            to_point = coordinates[to_id]

            route_distance += optimizer_service.euclidean_distance_scaled_int(a=from_point, b=to_point)

        total_distance += route_distance

        print(
            f"{vehicle_id}: {route_distance / 1000:.3f}"
        )

    print(f"Total distance: {total_distance / 1000:.3f} distance units")


    return response



if __name__ == "__main__":
    main()
