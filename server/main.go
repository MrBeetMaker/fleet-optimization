package main

import (
	"log"
	"net"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"

	"google.golang.org/grpc"
)

func main() {

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}

	grpcServer := grpc.NewServer()

	fleetpb.RegisterFleetServiceServer(grpcServer, NewFleetServer())

	log.Println("Server listening on :50051")

	grpcServer.Serve(lis)
}
