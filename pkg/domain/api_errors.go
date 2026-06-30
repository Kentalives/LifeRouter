package domain

import "errors"

// ErrNATSRequest classifies request/reply send failures in the public clients.
var ErrNATSRequest = errors.New("nats request failed")

// ErrNATSResponse classifies failed response decoding or remote error responses.
var ErrNATSResponse = errors.New("nats response failed")

// ErrNATSSubscription classifies failed subscription setup in streaming helpers.
var ErrNATSSubscription = errors.New("nats subscription failed")

// ErrNATSPublish classifies failed request publication after a subscription is ready.
var ErrNATSPublish = errors.New("nats publish failed")

// ErrAgentFindPath classifies failures from Pathfinding.AgentFindPath.
var ErrAgentFindPath = errors.New("agent find path")

// ErrAgentNaivePathCost classifies failures from Pathfinding.AgentNaivePathCost.
var ErrAgentNaivePathCost = errors.New("agent naive path cost")

// ErrAgentPathSub classifies failures from Pathfinding.AgentPathSub.
var ErrAgentPathSub = errors.New("agent path subscription")

// ErrAgentCommUpdateMovementSpeed classifies UpdateMovementSpeed failures.
var ErrAgentCommUpdateMovementSpeed = errors.New("agent communicator update movement speed")

// ErrAgentCommBlockingWait classifies BlockingWait failures.
var ErrAgentCommBlockingWait = errors.New("agent communicator blocking wait")

// ErrAgentCommIsMoving classifies IsMoving failures.
var ErrAgentCommIsMoving = errors.New("agent communicator is moving")

// ErrAgentCommExitError classifies ExitError failures.
var ErrAgentCommExitError = errors.New("agent communicator exit error")

// ErrAgentCommTerminate classifies Terminate failures.
var ErrAgentCommTerminate = errors.New("agent communicator terminate")

// ErrAgentCommStop classifies Stop failures.
var ErrAgentCommStop = errors.New("agent communicator stop")

// ErrAgentCommMoveNCells classifies MoveNCells failures.
var ErrAgentCommMoveNCells = errors.New("agent communicator move n cells")

// ErrAgentCommMoveFMeters classifies MoveFMeters failures.
var ErrAgentCommMoveFMeters = errors.New("agent communicator move meters")

// ErrEmergencyStart classifies Emergency.Start failures.
var ErrEmergencyStart = errors.New("emergency start")

// ErrEmergencyStop classifies Emergency.Stop failures.
var ErrEmergencyStop = errors.New("emergency stop")

// ErrEmergencyFlowSub classifies Emergency.FlowSub failures.
var ErrEmergencyFlowSub = errors.New("emergency flow subscription")
