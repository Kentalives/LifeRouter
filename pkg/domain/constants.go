package domain

// CELL_SIZE_M is the default real-world size, in meters, represented by one
// grid cell. Runtime configuration can override the service grid cell size.
const CELL_SIZE_M = 0.2

// Cost is the integer traversal cost used by grids and pathfinding algorithms.
type Cost = int32

// Grid traversal costs use COST_EMPTY_SPACE as the normal movement unit.
//
// COST_UNREACHABLE is a sentinel high value and should not be added directly.
const COST_UNREACHABLE Cost = 1<<30 - 1

// COST_EMPTY_SPACE is the normal cost of traversing one free grid cell.
const COST_EMPTY_SPACE Cost = 10

// COST_AGENT is the default dynamic cost added by another agent.
const COST_AGENT Cost = COST_EMPTY_SPACE * 2

// COST_LIGHT_OBJECT is the default dynamic cost added by a light obstacle.
const COST_LIGHT_OBJECT Cost = COST_EMPTY_SPACE * 5

// COST_HEAVY_OBJECT is the default dynamic cost added by a heavy obstacle.
const COST_HEAVY_OBJECT Cost = COST_EMPTY_SPACE * 100

//////

// CellState is the public visualization state for one agent-path grid cell.
type CellState = uint8

const (
	// STATE_Unvisited marks a cell not processed by an agent path search.
	STATE_Unvisited CellState = iota

	// STATE_Queued marks a cell currently queued for expansion.
	STATE_Queued

	// STATE_Expanded marks a cell already expanded by the search.
	STATE_Expanded

	// STATE_Inconsistent marks a cell whose D* Lite state needs repair.
	STATE_Inconsistent

	// STATE_OnPath marks a cell on the current agent path.
	STATE_OnPath

	// STATE_Obstacle marks a blocked or unreachable cell.
	STATE_Obstacle
)

//////

// Direction is the encoded emergency flow direction for one grid cell.
type Direction = uint8

// Emergency direction constants encode the flow-field direction for each cell.
// DIR_IN and DIR_OUT represent portal transitions between floors.
const (
	// DIR_DOWN points toward the next row.
	DIR_DOWN = iota

	// DIR_UP points toward the previous row.
	DIR_UP

	// DIR_LEFT points toward the previous column.
	DIR_LEFT

	// DIR_RIGHT points toward the next column.
	DIR_RIGHT

	// DIR_DOWN_LEFT points diagonally down and left.
	DIR_DOWN_LEFT

	// DIR_DOWN_RIGHT points diagonally down and right.
	DIR_DOWN_RIGHT

	// DIR_UP_LEFT points diagonally up and left.
	DIR_UP_LEFT

	// DIR_UP_RIGHT points diagonally up and right.
	DIR_UP_RIGHT

	// DIR_IN points through an incoming portal between floors.
	DIR_IN

	// DIR_OUT points through an outgoing portal between floors.
	DIR_OUT

	// DIR_UNKNOWN means no valid flow direction is available.
	DIR_UNKNOWN
)

//////
