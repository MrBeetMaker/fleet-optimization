# Fleet Optimization

A distributed electric fleet simulation and route optimization platform built with **Go**, **Python**, **gRPC**, **PostgreSQL**, **OR-Tools**, and **XGBoost**.

The goal of this project is to build a realistic logistics backend inspired by modern fleet management systems. Simulated trucks operate as independent clients that communicate with a Go backend, while a separate Python optimization service generates delivery schedules under real-world constraints such as capacity, battery usage, charging stations, pickup and delivery ordering, and time windows.

## Planned architecture

```text
                   Browser (HTML/JS)
                          │
                      REST API
                          │
                 ┌────────▼────────┐
                 │   Go Backend    │
                 │ Fleet Service   │
                 │ Order Service   │
                 │ World Model     │
                 └───────┬─────────┘
                         │ gRPC
          ┌──────────────┴──────────────┐
          │              │              │
     Truck Clients       │        Python Optimizer
      (Go)               │         OR-Tools
                         │         XGBoost
                         │
                    PostgreSQL
```

## Current status

* [x] Repository initialized
* [x] gRPC interface defined (`fleet.proto`)
* [ ] Go gRPC server
* [ ] Truck registration
* [ ] Telemetry system
* [ ] World model
* [ ] PostgreSQL integration
* [ ] Python optimization service
* [ ] OR-Tools scheduling
* [ ] XGBoost prediction models
* [ ] Web interface
* [ ] Docker Compose

## Project structure

```text
fleet-optimization/
├── client/         Simulated truck clients (Go)
├── server/         Backend orchestrator (Go)
├── optimizer/      Optimization service (Python)
├── frontend/       Static web interface
├── database/       Database schema and migrations
├── proto/          gRPC definitions
├── README.md
└── LICENSE
```

## Core features

### Fleet simulation

Each truck maintains its own simulated state:

* Position
* Battery level
* Cargo capacity
* Current route
* Operational state (Idle, Driving, Charging, Waiting, Delivering)

### Orders

Orders contain:

* Pickup node
* Delivery node
* Cargo size
* Ready time
* Delivery deadline

### World model

The simulated world contains:

* Nodes
* Roads
* Charging stations
* Service times
* Charger capacities

The backend maintains its own operational view of the world based on truck telemetry, mirroring how real fleet management systems operate.

### Optimization

The optimization service will combine:

* Google OR-Tools for constrained route planning
* XGBoost for learned predictions such as energy consumption, travel time, and waiting time

## Technology stack

| Area             | Technology                 |
| ---------------- | -------------------------- |
| Backend          | Go                         |
| Communication    | gRPC + Protocol Buffers    |
| Database         | PostgreSQL                 |
| Optimization     | OR-Tools                   |
| Machine Learning | Python, XGBoost            |
| Frontend         | HTML, JavaScript           |
| Visualization    | Python (Matplotlib/Plotly) |
| Deployment       | Docker Compose (planned)   |

## Roadmap

### Phase 1

* gRPC communication
* Truck registration
* Telemetry
* Basic commands

### Phase 2

* World model
* Orders
* Scheduling API
* PostgreSQL

### Phase 3

* OR-Tools optimization
* Battery constraints
* Charging stations
* Time windows

### Phase 4

* XGBoost prediction models
* Traffic simulation
* Energy consumption prediction
* Waiting time prediction

### Phase 5

* Web dashboard
* Live truck visualization
* Docker deployment
* Cloud deployment (GCP)

## License

MIT License.
