from concurrent import futures
import grpc
from proto import fleet_pb2
from proto import optim_pb2
from proto import optim_pb2_grpc

class OptimizerService(optim_pb2_grpc.OptimizerServiceServicer):

    def CreateRoute(self, request, context):
        print("Number of trucks:", request.nrOfTrucks)

        route = list()
        orders_included = list()

        print("\nOrders:")
        for order in request.orders:
            print(
                "id:", order.id,
                "pickUp:", order.pickUp,
                "dropOff:", order.dropOff,
                "size:", order.size,
            )

            if order.pickUp < 0:
                print(f"WARNING: Order {order.id} has invalid pickup node {order.pickUp}")
                continue

            if order.dropOff < 0:
                print(f"WARNING: Order {order.id} has invalid dropoff node {order.dropOff}")
                continue

            if order.pickUp not in route:
                route.append(order.pickUp)

            if order.dropOff not in route:
                route.append(order.dropOff)

            if order.id not in orders_included:
                orders_included.append(order.id)

        return optim_pb2.CreateRouteResponse(
            routes={
                0: fleet_pb2.Route(nodes=route)
            },
            order_ids={0: optim_pb2.OrderIds(ids=orders_included)
            }
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
