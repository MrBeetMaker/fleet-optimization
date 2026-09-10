package main

import (
	"context"
	"log"
	"net"
	"time"

	fleetpb "github.com/MrBeetMaker/fleet-optimization/proto"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func retry[T any](fn func() (T, error), maxRetries int, backoffDuration time.Duration) (T, error) {
	var output T
	var err error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var value T
		value, err = fn()
		if err == nil {
			return value, nil
		}

		if attempt < maxRetries {
			log.Printf("Attempt %d/%d failed: %v. Retrying...", attempt, maxRetries, err)
			time.Sleep(backoffDuration)
		}
	}

	return output, err
}

func loadEnvironment() {
	log.Print("Loading environment: ")
	godotenv.Load()
	log.Print("Success.")
}

func connectDatabase(maxRetries int, backoffDuration time.Duration) *Database {
	log.Print("Connecting to database: ")

	db, err := retry(func() (*Database, error) { return OpenDatabase(context.Background()) }, maxRetries, backoffDuration)
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	log.Print("Success.")
	return db
}

func startListener(maxRetries int, backoffDuration time.Duration) net.Listener {
	log.Print("Starting TCP listener on :50051: ")

	lis, err := retry(func() (net.Listener, error) { return net.Listen("tcp", ":50051") }, maxRetries, backoffDuration)
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	log.Print("Success.")
	return lis
}

func connectOptimizer(maxRetries int, backoffDuration time.Duration) (*grpc.ClientConn, fleetpb.OptimizerServiceClient) {
	log.Print("Connecting to optimizer service: ")

	optimizerConn, err := retry(func() (*grpc.ClientConn, error) {
		return grpc.NewClient("localhost:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
	}, maxRetries, backoffDuration)
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	log.Print("Success.")
	return optimizerConn, fleetpb.NewOptimizerServiceClient(optimizerConn)
}

func createFleetServer(db *Database, optimizerClient fleetpb.OptimizerServiceClient, maxRetries int, backoffDuration time.Duration) *FleetServer {
	log.Print("Starting fleet server: ")

	fleetServer, err := retry(func() (*FleetServer, error) { return NewFleetServer(db, optimizerClient) }, maxRetries, backoffDuration)
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	log.Print("Success.")
	return fleetServer
}

func main() {
	const (
		maxRetries      = 3
		backoffDuration = 1000 * time.Millisecond
	)

	loadEnvironment()

	db := connectDatabase(maxRetries+3, backoffDuration*6)
	defer db.Close()

	lis := startListener(maxRetries, backoffDuration)
	defer lis.Close()

	optimizerConn, optimizerClient := connectOptimizer(maxRetries, backoffDuration)
	defer optimizerConn.Close()

	log.Print("Creating gRPC server: ")
	grpcServer := grpc.NewServer()
	log.Print("Success.")

	fleetServer := createFleetServer(db, optimizerClient, maxRetries, backoffDuration)

	log.Print("Registering fleet service: ")
	fleetpb.RegisterFleetServiceServer(grpcServer, fleetServer)
	log.Print("Success.")

	log.Print("Starting periodic route planning: ")
	go fleetServer.RunPeriodicRoutePlanning()
	log.Print("Success.")

	log.Print("Starting gRPC server: ")
	if err := grpcServer.Serve(lis); err != nil {
		log.Printf("ERROR: %v", err)
		log.Fatal(err)
	}
}
