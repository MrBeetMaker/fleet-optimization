package main

import (
	"context"
	"fmt"
	"log"
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

// TruckRecord is a representation of a truck.
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
		return nil, fmt.Errorf("DATABASE: DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("DATABASE: Could not parse database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("DATABASE: Could not create database pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("DATABASE: Could not ping database: %w", err)
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
		return nil, fmt.Errorf("DATABASE: Could not query points: %w", err)
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
			return nil, fmt.Errorf("DATABASE: Could not scan point: %w", err)
		}

		points[point.ID] = &point
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("DATABASE: Could not iterate points: %w", err)
	}

	log.Printf("DATABASE: Got %d points.", len(points))

	return points, nil
}

// GetTrucks loads the current persisted state of all trucks.
//
// FleetServer should normally call this during startup to reconstruct
// its in-memory operational state.
func (db *Database) GetTrucks(ctx context.Context) (map[int32]*TruckRecord, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT
			t.id,
			COALESCE(telemetry.state, 'OFFLINE'),
			COALESCE(telemetry.x, 0),
			COALESCE(telemetry.y, 0)
		FROM trucks t
		LEFT JOIN LATERAL (
			SELECT
				state,
				x,
				y
			FROM truck_telemetry
			WHERE truck_id = t.id
			ORDER BY recorded_at DESC, id DESC
			LIMIT 1
		) telemetry ON true
		ORDER BY t.id
	`)
	if err != nil {
		return nil, fmt.Errorf("DATABASE: query trucks: %w", err)
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
			return nil, fmt.Errorf("DATABASE: scan truck: %w", err)
		}

		trucks[truck.ID] = &truck
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("DATABASE: iterate trucks: %w", err)
	}

	log.Printf("DATABASE: Got %d trucks.", len(trucks))

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
		return nil, fmt.Errorf("DATABASE: Could not query active orders: %w", err)
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
			return nil, fmt.Errorf("DATABASE: Could not scan active order: %w", err)
		}

		orders[order.ID] = &order
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("DATABASE: Could not iterate active orders: %w", err)
	}
	log.Printf("DATABASE: Got %d active orders.", len(orders))

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
		return nil, fmt.Errorf("DATABASE: Could not query pending orders: %w", err)
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
			return nil, fmt.Errorf("DATABASE: Could not scan pending order: %w", err)
		}

		orders[order.ID] = &order
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("DATABASE: Could not iterate pending orders: %w", err)
	}

	log.Printf("DATABASE: Got %d pending orders.", len(orders))

	return orders, nil
}

// AssignOrder atomically transitions an order:
//
//	PENDING -> ASSIGNED
//
// The assignment is only successful if the order is still pending.
// This protects against stale application state.
func (db *Database) AssignOrder(ctx context.Context, orderID int64, truckID int32, eventAt time.Time) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE orders
		SET
			state = $1,
			truck_id = $2,
			updated_at = $3
		WHERE id = $4
		  AND state = $5
	`,
		OrderAssigned,
		truckID,
		eventAt,
		orderID,
		OrderPending,
	)
	if err != nil {
		return fmt.Errorf("DATABASE: Could not assign order %d to truck %d: %w",
			orderID, truckID, err)
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("DATABASE: Could not assign order %d to truck %d: order not found or not pending",
			orderID,
			truckID,
		)
	}

	log.Printf("DATABASE: Assigned order %d to truck %d.", orderID, truckID)

	return nil
}

// MarkOrderPickedUp atomically transitions an order:
//
//  ASSIGNED -> PICKED_UP
//
// The truck ID must match the truck currently assigned to the order.
func (db *Database) MarkOrderPickedUp(ctx context.Context, orderID int64, truckID int32, eventAt time.Time) error {

	tag, err := db.pool.Exec(ctx, `
        UPDATE orders
        SET
            state = $1,
            picked_up_at = COALESCE(picked_up_at, $2),
            updated_at = $2
        WHERE id = $3
          AND state = $4
          AND truck_id = $5
    `,
		OrderPickedUp,
		eventAt,
		orderID,
		OrderAssigned,
		truckID,
	)
	if err != nil {
		return fmt.Errorf("DATABASE: Could not mark order %d picked up by truck %d: %w",
			orderID,
			truckID,
			err,
		)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("DATABASE: Could not mark order %d picked up by truck %d: order not found, not assigned, or assigned to another truck",
			orderID,
			truckID,
		)
	}
	log.Printf("DATABASE: Marked order %d as picked up by truck %d.", orderID, truckID)
	return nil
}

// MarkOrderDelivered atomically transitions an order:
//
//  PICKED_UP -> DELIVERED
//
// The truck ID must match the truck currently assigned to the order.
func (db *Database) MarkOrderDelivered(ctx context.Context, orderID int64, truckID int32, eventAt time.Time) error {
	tag, err := db.pool.Exec(ctx, `
        UPDATE orders
        SET
            state = $1,
            delivered_at = COALESCE(delivered_at, $2),
            updated_at = $2
        WHERE id = $3
          AND state = $4
          AND truck_id = $5
    `,
		OrderDelivered,
		eventAt,
		orderID,
		OrderPickedUp,
		truckID,
	)
	if err != nil {
		return fmt.Errorf("DATABASE: Could not mark order %d delivered by truck %d: %w",
			orderID,
			truckID,
			err,
		)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("DATABASE: Could not mark order %d delivered by truck %d: order not found, not picked up, or assigned to another truck",
			orderID,
			truckID,
		)
	}
	log.Printf("DATABASE: Marked order %d as delivered by truck %d", orderID, truckID)
	return nil
}

func (db *Database) InsertTruckTelemetry(ctx context.Context, truckID int32, state TruckState, x float64, y float64, recordedAt time.Time) error {
	_, err := db.pool.Exec(ctx, `
        INSERT INTO truck_telemetry (
            truck_id,
            state,
            x,
            y,
            recorded_at
        )
        VALUES ($1, $2, $3, $4, $5)
    `,
		truckID,
		state,
		x,
		y,
		recordedAt,
	)
	if err != nil {
		return fmt.Errorf("DATABASE: Could not record telemetry for truck %d: %w",
			truckID,
			err,
		)
	}

	return nil
}
