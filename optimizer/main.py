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

    @staticmethod
    def euclidean_distance(a, b):
        # OR-Tools routing costs should be integers.
        return round(math.hypot(a[0] - b[0], a[1] - b[1]) * 1000)

    def InitializeOptimizer(self, request, context):

        print(f"Optimizer received {len(request.points)} nodes.")

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
        )

        valid_pickup = order.pickUp in self.points
        valid_dropoff = order.dropOff in self.points

        if not valid_pickup:
            print(f"WARNING: Order {order.id} has invalid pickup node {order.pickUp}")
            return False
        if not valid_dropoff:
            print(f"WARNING: Order {order.id} has invalid dropoff node {order.dropOff}")
            return False

        return True

    def CreateRoute(self, request, context):
        start = perf_counter()
        print(f"Number of trucks: {request.nrOfTrucks}")
        nr_of_trucks = request.nrOfTrucks

        starts = [0 for _ in range(nr_of_trucks)]
        ends = [0 for _ in range(nr_of_trucks)]

        orders_included = list()
        node_map = dict()           # Store index for real node id
        points = list()
        precedence_pairs = list()

        for node in starts:
            if node not in node_map:
                node_map[node] = len(points)
                points.append(self.points[node])

        print("Validating orders and collecting relevant nodes.")

        for order in request.orders:
            if self.validate_order(order):
                for node in (order.pickUp, order.dropOff):
                    if node not in node_map:
                        node_map[node] = len(points)
                        points.append(self.points[node])

                precedence_pairs.append((node_map[order.pickUp], node_map[order.dropOff]))
                orders_included.append(order.id)

        if len(orders_included) == 0:
            return optim_pb2.CreateRouteResponse(
                routes={0: fleet_pb2.Route(nodes=[])},
                order_ids={0: optim_pb2.OrderIds(ids=[])}
            )

        # Setup OR-tools
        manager = pywrapcp.RoutingIndexManager(
            len(points),
            nr_of_trucks,
            starts,
            ends,
        )
        routing = pywrapcp.RoutingModel(manager)

        # Precalculate distances
        distance_matrix = [[self.euclidean_distance(a, b) for b in points] for a in points]

        # Register callback function for travel costs
        def distance_callback(node_a: int, node_b: int):
            indx_a = manager.IndexToNode(node_a)
            indx_b = manager.IndexToNode(node_b)
            return distance_matrix[indx_a][indx_b]

        transit_callback_index = routing.RegisterTransitCallback(distance_callback)

        # Minimize total route distance.
        routing.SetArcCostEvaluatorOfAllVehicles(transit_callback_index)

        # Add a dimension whose cumulative value represents distance traveled
        # from the start of a route.
        routing.AddDimension(
            transit_callback_index,
            0,        # no slack
            10**12,   # maximum route distance
            True,     # start cumul at zero
            "Distance",
        )

        distance_dimension = routing.GetDimensionOrDie("Distance")

        # Add precedence constraints.
        for node_a, node_b in precedence_pairs:
            before_index = manager.NodeToIndex(node_a)
            after_index = manager.NodeToIndex(node_b)

            routing.AddPickupAndDelivery(before_index, after_index)

            # Both nodes must use the same vehicle.
            routing.solver().Add(routing.VehicleVar(before_index) == routing.VehicleVar(after_index))

            # The vehicle must reach node a before node b
            routing.solver().Add(distance_dimension.CumulVar(before_index) <= distance_dimension.CumulVar(after_index))

        # Search configuration.
        search_parameters = pywrapcp.DefaultRoutingSearchParameters()
        search_parameters.first_solution_strategy = routing_enums_pb2.FirstSolutionStrategy.PATH_CHEAPEST_ARC
        search_parameters.local_search_metaheuristic = routing_enums_pb2.LocalSearchMetaheuristic.GUIDED_LOCAL_SEARCH
        search_parameters.time_limit.seconds = 10

        solution = routing.SolveWithParameters(search_parameters)

        if solution:
            reverse_node_map = {v: k for k, v in node_map.items()}      # Restore to real node id's

            routes = dict()

            for vehicle_id in range(nr_of_trucks):
                index = routing.Start(vehicle_id)
                route = list()

                while not routing.IsEnd(index):
                    route.append(reverse_node_map[manager.IndexToNode(index)])
                    index = solution.Value(routing.NextVar(index))

                route.append(reverse_node_map[manager.IndexToNode(index)])

                print(f"Vehicle {vehicle_id}: {route}")

                routes.update({vehicle_id: fleet_pb2.Route(nodes=route)})

            print(
                "Total distance:",
                solution.ObjectiveValue() / 1000,
            )

            duration = perf_counter() - start

            print(f"Finished in {duration:.2f} seconds.")

            return optim_pb2.CreateRouteResponse(
                routes=routes,
                order_ids={0: optim_pb2.OrderIds(ids=orders_included)}
            )

        else:
            print("No solution found.")

            duration = perf_counter() - start
            print(f"Finished in {duration:.2f} seconds.")

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


if __name__ == "__main__":
    main()
