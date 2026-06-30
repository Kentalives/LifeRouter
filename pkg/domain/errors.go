package domain

import (
	"errors"
	"fmt"

	"github.com/midtxwn/geotruth/pkg/messages"
)

// ErrAgentExitedWithMetersLeft reports that a meter-based movement request
// could not finish because the agent pathfinding run ended first.
var ErrAgentExitedWithMetersLeft = errors.New("agent exited but meters remained")

// ErrAgentCommNotFound reports that an agent communicator ID is not active or retained.
var ErrAgentCommNotFound = errors.New("Agent communiator not found")

// ErrAgentNoPath reports that no traversable route exists to the requested goal.
var ErrAgentNoPath = errors.New("agent found no possible path")

// ErrAgentTerminated reports that an agent pathfinding run was explicitly canceled.
var ErrAgentTerminated = errors.New("agent pathfinding terminated")

// ErrAgentStillRunning reports that a terminal result was requested too early.
var ErrAgentStillRunning = errors.New("agent pathfinding still running")

// ErrEmergencyStillRunning reports that a new emergency run was requested while one is active.
var ErrEmergencyStillRunning = errors.New("emergency pathfinding still running")

// ErrDispatcherShuttingDown reports that the service is draining and no longer accepts new work.
var ErrDispatcherShuttingDown = errors.New("pathfinding dispatcher shutting down")

const (
	errAgentExitedWithMetersLeft = iota
	errAgentCommNotFound
	errAgentNoPath
	errAgentTerminated
	errAgentStillRunning
	errEmergencyStillRunning
	errDispatcherShuttingDown
)

func init() {
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errAgentExitedWithMetersLeft), ErrAgentExitedWithMetersLeft)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errAgentCommNotFound), ErrAgentCommNotFound)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errAgentNoPath), ErrAgentNoPath)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errAgentTerminated), ErrAgentTerminated)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errAgentStillRunning), ErrAgentStillRunning)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errEmergencyStillRunning), ErrEmergencyStillRunning)
	messages.MustRegisterError(fmt.Sprintf("pthf_%d", errDispatcherShuttingDown), ErrDispatcherShuttingDown)
}
