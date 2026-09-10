-- FLEET OPTIMIZATION DATABASE
-- PostgreSQL 18

-------------------------------------------------
-- Points
CREATE TABLE points (
    id INTEGER PRIMARY KEY,
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL
);

-------------------------------------------------
-- Trucks
CREATE TABLE trucks (
    id INTEGER PRIMARY KEY,

    state TEXT NOT NULL
        CHECK (state IN (
            'IDLE',
            'DRIVING',
            'PICKING_UP',
            'DELIVERING',
            'OFFLINE'
        )),

    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-------------------------------------------------
-- Orders
CREATE TABLE orders (
    id BIGINT PRIMARY KEY,

    pickup_id INTEGER NOT NULL,
    dropoff_id INTEGER NOT NULL,

    size INTEGER NOT NULL
        CHECK (size > 0),

    weight DOUBLE PRECISION NOT NULL
        CHECK (weight > 0),

    state TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (state IN (
            'PENDING',
            'ASSIGNED',
            'PICKED_UP',
            'DELIVERED'
        )),

    truck_id INTEGER,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    picked_up_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,

    FOREIGN KEY (pickup_id)
        REFERENCES points(id),

    FOREIGN KEY (dropoff_id)
        REFERENCES points(id),

    FOREIGN KEY (truck_id)
        REFERENCES trucks(id)
        ON DELETE SET NULL
);


-------------------------------------------------
-- INDEXES
-- These are things we want extra quick access to

-- Pending orders
CREATE INDEX idx_orders_pending
    ON orders (state)
    WHERE state = 'PENDING';

-- A truck's orders
CREATE INDEX idx_orders_truck
    ON orders (truck_id)
    WHERE truck_id IS NOT NULL;

-- Active orders
CREATE INDEX idx_orders_active
    ON orders (state)
    WHERE state != 'DELIVERED';


-------------------------------------------------

INSERT INTO points (id, x, y) VALUES
    (0,  0.0,  0.0),
    (1,  1.0,  1.0),
    (2,  1.8,  1.4),
    (3,  2.5,  2.1),
    (4,  1.6,  2.6),
    (5,  0.8,  2.0),
    (6,  2.1,  0.7),
    (7,  3.0,  1.2),
    (8,  3.4,  2.3),
    (9,  7.0,  1.0),
    (10, 8.2,  1.3),
    (11, 8.8,  2.2),
    (12, 8.1,  3.0),
    (13, 6.9,  3.2),
    (14, 6.1, 2.3),
    (15, 6.2, 1.4),
    (16, 7.5, 2.0),
    (17, 13.0, 7.0),
    (18, 14.2, 7.4),
    (19, 15.0, 8.3),
    (20, 14.6, 9.4),
    (21, 13.3, 9.8),
    (22, 12.2, 9.1),
    (23, 11.8, 8.0),
    (24, 12.5, 7.2),
    (25, 4.0, 3.5),
    (26, 5.0, 4.0),
    (27, 6.0, 4.8),
    (28, 7.2, 5.1),
    (29, 8.5, 5.8),
    (30, 9.7, 6.2),
    (31, 10.8, 6.8),
    (32, 11.5, 7.4);

-------------------------------------------------
INSERT INTO trucks (id, state, x, y) VALUES
    (0, 'OFFLINE', 0.0, 0.0),
    (1, 'OFFLINE', 1.0, 1.0),
    (2, 'OFFLINE', 2.1, 0.7),
    (3, 'OFFLINE', 7.0, 1.0),
    (4, 'OFFLINE', 8.2, 1.3),
    (5, 'OFFLINE', 13.0, 7.0);

-------------------------------------------------
INSERT INTO orders (
    id,
    pickup_id,
    dropoff_id,
    size,
    weight,
    state,
    truck_id
) VALUES
    (1001,  1, 17, 3,  18.5, 'PENDING', NULL),
    (1002,  2, 18, 5,  42.0, 'PENDING', NULL),
    (1003,  3, 19, 2,  11.0, 'PENDING', NULL),
    (1004,  4, 20, 7,  63.5, 'PENDING', NULL),
    (1005,  5, 21, 4,  31.0, 'PENDING', NULL),
    (1006,  6, 22, 8,  77.0, 'PENDING', NULL),
    (1007,  7, 23, 1,   7.5, 'PENDING', NULL),
    (1008,  8, 24, 6,  52.0, 'PENDING', NULL),
    (1009,  9, 31, 4,  35.0, 'PENDING', NULL),
    (2011, 6, 28, 11,  11.0, 'DELIVERED', NULL);
    (2022, 16, 9, 9,  71.0, 'DELIVERED', NULL);
    (2033, 18, 2, 4,  41.0, 'DELIVERED', NULL);
    (2044, 14, 14, 4,  21.0, 'DELIVERED', NULL);
    (2055, 2, 5, 7,  30.0, 'DELIVERED', NULL);
-------------------------------------------------

