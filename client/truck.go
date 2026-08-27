package simulation

type TruckState int

const (
	IDLE TruckState = iota
	DRIVING
	CHARGING
	WAITING
	DELIVERING
)

var stateName = map[TruckState]string{
	IDLE:       "idle",
	DRIVING:    "driving",
	CHARGING:   "charging",
	WAITING:    "waiting",
	DELIVERING: "delivering",
}

func (ss TruckState) String() string {
	return stateName[ss]
}

type truck struct {
	id              int
	orders          []int
	storageCapacity int
	battery         float64 // Percentage between 0 and 100
	status          TruckState
	currentNode     int // Change to node later
	x               float64
	y               float64
}

func (tr truck) drive(x float64, y float64) // Change to node later
func (tr truck) sendTelemetry()

type telemetry struct {
	truckId int
	// timestamp datetime
	x       float64
	y       float64
	battery float64
}
