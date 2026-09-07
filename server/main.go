package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
)

func main() {

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}

	optimizerConn, err := grpc.NewClient(
		"localhost:50052",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer optimizerConn.Close()

	optimizerClient := fleetpb.NewOptimizerServiceClient(optimizerConn)

	grpcServer := grpc.NewServer()

	fleetServer := NewFleetServer(optimizerClient)

	fleetpb.RegisterFleetServiceServer(
		grpcServer,
		fleetServer,
	)

	go fleetServer.RunPeriodicRoutePlanning()

	log.Println("Server listening on :50051")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
