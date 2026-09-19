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
    id INTEGER PRIMARY KEY
    -- Here we can add truck specs later on
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
-- Telemetry history

CREATE TABLE truck_telemetry (
    id BIGSERIAL PRIMARY KEY,
    truck_id INTEGER NOT NULL,
    state TEXT NOT NULL
        CHECK (state IN (
            'IDLE',
            'DRIVING',
            'DELIVERING',
            'CHARGING',
            'WAITING',
            'OFFLINE'
        )),
    x DOUBLE PRECISION NOT NULL,
    y DOUBLE PRECISION NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL,

    FOREIGN KEY (truck_id)
        REFERENCES trucks(id)
        ON DELETE CASCADE
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

-- Telemetry
CREATE INDEX idx_truck_telemetry_truck_time
    ON truck_telemetry (truck_id, recorded_at DESC);


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
    (32, 11.5, 7.4),
    (33, 17.8, 3.8),
    (34, 18.4, 4.2),
    (35, 19.1, 3.6),
    (36, 19.7, 4.8),
    (37, 18.8, 5.5),
    (38, 17.5, 5.0),
    (39, 20.3, 4.1),
    (40, 21.0, 5.2),
    (41, 19.2, 11.4),
    (42, 20.1, 12.0),
    (43, 21.0, 12.7),
    (44, 20.4, 13.6),
    (45, 19.0, 13.1),
    (46, 21.8, 11.8),
    (47, 3.2, 9.1),
    (48, 4.0, 9.8),
    (49, 4.8, 10.5),
    (50, 3.7, 11.2),
    (51, 5.3, 9.4),
    (52, 5.9, 10.8),
    (53, 9.8, 9.2),
    (54, 16.5, 1.5),
    (55, 22.8, 14.1),
    (56, 1.2, 7.8),
    (57, 7.1, 12.4),
    (58, 11.9, 3.8),
    (59, 15.8, 11.2),
    (60, 23.5, 6.7),
    (61, 24.6, 15.2),
    (62, 13.8, 0.9),
    (63, 0.4, 4.9),
    (64, 25.8, 10.3);


-------------------------------------------------
INSERT INTO trucks (id) VALUES
    (0),
    (1),
    (2),
    (3),
    (4),
    (5);

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
    (1009,  9, 16, 4,  5.0, 'PENDING', NULL),
    (1010,  3, 9, 2,  15.0, 'PENDING', NULL),
    (1011,  5, 22, 8,  42.0, 'PENDING', NULL),
    (1012,  3, 31, 4,  12.0, 'PENDING', NULL),
    (1013,  1, 2, 5,  1.0, 'PENDING', NULL),
    (1014,  20, 22, 4,  18.0, 'PENDING', NULL),
    (1015,  3, 10, 1,  8.0, 'PENDING', NULL),
    (1016, 33, 41, 4, 26.0, 'PENDING', NULL),
    (1017, 34, 20, 3, 19.5, 'PENDING', NULL),
    (1018, 35, 55, 7, 58.0, 'PENDING', NULL),
    (1019, 36, 42, 5, 34.0, 'PENDING', NULL),
    (1020, 37, 18, 2, 12.0, 'PENDING', NULL),
    (1021, 38, 54, 6, 45.0, 'PENDING', NULL),
    (1022, 39, 43, 8, 71.0, 'PENDING', NULL),
    (1023, 40, 24, 3, 18.0, 'PENDING', NULL),
    (1024, 41, 60, 5, 39.0, 'PENDING', NULL),
    (1025, 42, 61, 9, 82.0, 'PENDING', NULL),
    (1026, 43, 44, 2, 9.5, 'PENDING', NULL),
    (1027, 44, 59, 4, 23.0, 'PENDING', NULL),
    (1028, 45, 53, 6, 41.0, 'PENDING', NULL),
    (1029, 46, 21, 5, 36.5, 'PENDING', NULL),
    (1030, 47, 49, 2, 8.0, 'PENDING', NULL),
    (1031, 48, 52, 7, 54.0, 'PENDING', NULL),
    (1032, 49, 17, 4, 22.0, 'PENDING', NULL),
    (1033, 50, 57, 3, 17.0, 'PENDING', NULL),
    (1034, 51, 30, 6, 47.0, 'PENDING', NULL),
    (1035, 52, 64, 8, 69.0, 'PENDING', NULL),
    (1036, 53, 22, 2, 11.0, 'PENDING', NULL),
    (1037, 54, 10, 5, 31.0, 'PENDING', NULL),
    (1038, 55, 46, 4, 27.0, 'PENDING', NULL),
    (1039, 56, 48, 3, 14.0, 'PENDING', NULL),
    (1040, 57, 62, 6, 44.0, 'PENDING', NULL),
    (1041, 58, 33, 4, 24.0, 'PENDING', NULL),
    (1042, 64, 45, 9, 87.0, 'PENDING', NULL),
    (2011, 6, 28, 11,  19.0, 'DELIVERED', NULL),
    (2022, 16, 9, 9,  71.0, 'DELIVERED', NULL),
    (2033, 18, 2, 4,  41.0, 'DELIVERED', NULL),
    (2044, 14, 14, 4,  21.0, 'DELIVERED', NULL),
    (2055, 2, 5, 7,  30.0, 'DELIVERED', NULL);
-------------------------------------------------

