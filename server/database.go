package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderState string

const (
	OrderPending   OrderState = "PENDING"
	OrderAssigned  OrderState = "ASSIGNED"
	OrderPickedUp  OrderState = "PICKED_UP"
	OrderDelivered OrderState = "DELIVERED"
)

type TruckState string

const (
	TruckIdle    TruckState = "IDLE"
	TruckDriving TruckState = "DRIVING"
	// TruckPickingUp  TruckState = "PICKING_UP"
	TruckDelivering TruckState = "DELIVERING"
	TruckCharging   TruckState = "CHARGING"
	TruckWaiting    TruckState = "WAITING"
	TruckOffline    TruckState = "OFFLINE"
)

// Database owns the PostgreSQL connection pool.
// The pool is created once and shared by the application.
type Database struct {
	pool *pgxpool.Pool
}

// PointRecord is the database representation of a point.
type PointRecord struct {
	ID int32
	X  float64
	Y  float64
}

// TruckRecord is the database representation of a truck.
type TruckRecord struct {
	ID    int32
	State TruckState
	X     float64
	Y     float64
}

// OrderRecord is the database representation of an order.
type OrderRecord struct {
	ID        int64
	PickupID  int32
	DropoffID int32
	Size      int32
	Weight    float64
	State     OrderState
	TruckID   *int32
}

// OpenDatabase creates and verifies the application's PostgreSQL
// connection pool.
//
// The caller owns the returned Database and is responsible for
// calling Close when the application shuts down.
func OpenDatabase(ctx context.Context) (*Database, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Database{
		pool: pool,
	}, nil
}

// Close releases all connections owned by the database pool.
func (db *Database) Close() {
	if db == nil || db.pool == nil {
		return
	}

	db.pool.Close()
}

// GetPoints loads all points.
//
// Points are static world data, so FleetServer should normally call
// this once during startup rather than repeatedly during operation.
func (db *Database) GetPoints(ctx context.Context) (map[int32]*PointRecord, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			id,
			x,
			y
		FROM points
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query points: %w", err)
	}
	defer rows.Close()

	points := make(map[int32]*PointRecord)

	for rows.Next() {
		var point PointRecord

		if err := rows.Scan(
			&point.ID,
			&point.X,
			&point.Y,
		); err != nil {
			return nil, fmt.Errorf("scan point: %w", err)
		}

		points[point.ID] = &point
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate points: %w", err)
	}

	return points, nil
}

// GetTrucks loads the current persisted state of all trucks.
//
// FleetServer should normally call this during startup to reconstruct
// its in-memory operational state.
func (db *Database) GetTrucks(ctx context.Context) (map[int32]*TruckRecord, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			id,
			state,
			x,
			y
		FROM trucks
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query trucks: %w", err)
	}
	defer rows.Close()

	trucks := make(map[int32]*TruckRecord)

	for rows.Next() {
		var truck TruckRecord

		if err := rows.Scan(
			&truck.ID,
			&truck.State,
			&truck.X,
			&truck.Y,
		); err != nil {
			return nil, fmt.Errorf("scan truck: %w", err)
		}

		trucks[truck.ID] = &truck
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trucks: %w", err)
	}

	return trucks, nil
}

// GetActiveOrders loads every order that has not yet been delivered.
//
// This is intended for startup/recovery so that FleetServer
// can reconstruct orders that were already assigned or
// picked up before a restart.
func (db *Database) GetActiveOrders(ctx context.Context) (map[int64]*OrderRecord, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			id,
			pickup_id,
			dropoff_id,
			size,
			weight,
			state,
			truck_id
		FROM orders
		WHERE state != $1
		ORDER BY created_at, id
	`, OrderDelivered)
	if err != nil {
		return nil, fmt.Errorf("query active orders: %w", err)
	}
	defer rows.Close()

	orders := make(map[int64]*OrderRecord)

	for rows.Next() {
		var order OrderRecord

		if err := rows.Scan(
			&order.ID,
			&order.PickupID,
			&order.DropoffID,
			&order.Size,
			&order.Weight,
			&order.State,
			&order.TruckID,
		); err != nil {
			return nil, fmt.Errorf("scan active order: %w", err)
		}

		orders[order.ID] = &order
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active orders: %w", err)
	}

	return orders, nil
}

// GetPendingOrders loads only orders that are waiting to be assigned.
//
// This should be used during normal operation when FleetServer needs
// additional work. Delivered and already-assigned orders are not
// repeatedly fetched.
func (db *Database) GetPendingOrders(ctx context.Context) (map[int64]*OrderRecord, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			id,
			pickup_id,
			dropoff_id,
			size,
			weight,
			state,
			truck_id
		FROM orders
		WHERE state = $1
		ORDER BY created_at, id
	`, OrderPending)
	if err != nil {
		return nil, fmt.Errorf("query pending orders: %w", err)
	}
	defer rows.Close()

	orders := make(map[int64]*OrderRecord)

	for rows.Next() {
		var order OrderRecord

		if err := rows.Scan(
			&order.ID,
			&order.PickupID,
			&order.DropoffID,
			&order.Size,
			&order.Weight,
			&order.State,
			&order.TruckID,
		); err != nil {
			return nil, fmt.Errorf("scan pending order: %w", err)
		}

		orders[order.ID] = &order
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending orders: %w", err)
	}

	return orders, nil
}

// AssignOrder atomically transitions an order:
//
//	PENDING -> ASSIGNED
//
// The assignment is only successful if the order is still pending.
// This protects against stale application state.
func (db *Database) AssignOrder(ctx context.Context, orderID int64, truckID int32) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE orders
		SET
			state = $1,
			truck_id = $2,
			updated_at = NOW()
		WHERE id = $3
		  AND state = $4
	`,
		OrderAssigned,
		truckID,
		orderID,
		OrderPending,
	)
	if err != nil {
		return fmt.Errorf("assign order %d to truck %d: %w",
			orderID, truckID, err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"assign order %d to truck %d: order not found or not pending",
			orderID,
			truckID,
		)
	}

	return nil
}

// MarkOrderPickedUp atomically transitions an order:
//
//	ASSIGNED -> PICKED_UP
//
// The truck ID must match the truck currently assigned to the order.
func (db *Database) MarkOrderPickedUp(ctx context.Context, orderID int64, truckID int32) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE orders
		SET
			state = $1,
			picked_up_at = COALESCE(picked_up_at, NOW()),
			updated_at = NOW()
		WHERE id = $2
		  AND state = $3
		  AND truck_id = $4
	`,
		OrderPickedUp,
		orderID,
		OrderAssigned,
		truckID,
	)
	if err != nil {
		return fmt.Errorf(
			"mark order %d picked up by truck %d: %w",
			orderID,
			truckID,
			err,
		)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"mark order %d picked up by truck %d: order not found, not assigned, or assigned to another truck",
			orderID,
			truckID,
		)
	}

	return nil
}

// MarkOrderDelivered atomically transitions an order:
//
//	PICKED_UP -> DELIVERED
//
// The truck ID must match the truck currently assigned to the order.
func (db *Database) MarkOrderDelivered(ctx context.Context, orderID int64, truckID int32) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE orders
		SET
			state = $1,
			delivered_at = COALESCE(delivered_at, NOW()),
			updated_at = NOW()
		WHERE id = $2
		  AND state = $3
		  AND truck_id = $4
	`,
		OrderDelivered,
		orderID,
		OrderPickedUp,
		truckID,
	)
	if err != nil {
		return fmt.Errorf(
			"mark order %d delivered by truck %d: %w",
			orderID,
			truckID,
			err,
		)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf(
			"mark order %d delivered by truck %d: order not found, not picked up, or assigned to another truck",
			orderID,
			truckID,
		)
	}

	return nil
}

// UpdateTruck persists the current operational state of a truck.
func (db *Database) UpdateTruck(ctx context.Context, truckID int32, state TruckState, x float64, y float64) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE trucks
		SET
			state = $1,
			x = $2,
			y = $3,
			updated_at = NOW()
		WHERE id = $4
	`,
		state,
		x,
		y,
		truckID,
	)
	if err != nil {
		return fmt.Errorf("update truck %d: %w", truckID, err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("update truck %d: truck not found", truckID)
	}

	return nil
}
