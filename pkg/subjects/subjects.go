package subjects

const (
	// EmergencyStart starts emergency flow-field routing.
	EmergencyStart = "emergency.start"

	// EmergencyStop stops emergency flow-field routing.
	EmergencyStop = "emergency.stop"

	// EmergencyFlowWatch subscribes to emergency flow snapshots.
	EmergencyFlowWatch = "emergency.flow_watch"
)

const (
	// PathfindingAgentFindPath starts adaptive pathfinding for an agent.
	PathfindingAgentFindPath = "pathfinding.agent_find_path"

	// PathfindingAgentNaivePathCost computes path cost without starting movement.
	PathfindingAgentNaivePathCost = "pathfinding.agent_naive_path_cost"

	// PathfindingAgentWatchPath subscribes to agent path snapshots.
	PathfindingAgentWatchPath = "pathfinding.agent_path_watch"
)

const (
	// AgentCommUpdateMovementSpeed changes a running agent's automatic movement speed.
	AgentCommUpdateMovementSpeed = "agent_comm.update_movement_speed"

	// AgentCommBlockingWait waits until an agent run finishes.
	AgentCommBlockingWait = "agent_comm.blocking_wait"

	// AgentCommIsMoving reports whether an agent run is active.
	AgentCommIsMoving = "agent_comm.is_moving"

	// AgentCommExitError returns the terminal error for a completed agent run.
	AgentCommExitError = "agent_comm.exit_error"

	// AgentCommTerminate cancels an agent run.
	AgentCommTerminate = "agent_comm.terminate"

	// AgentCommStop pauses automatic movement.
	AgentCommStop = "agent_comm.stop"

	// AgentCommMoveNCells advances an agent by up to N cells.
	AgentCommMoveNCells = "agent_comm.move_n_cells"

	// AgentCommMoveFMeters advances an agent by up to a distance in meters.
	AgentCommMoveFMeters = "agent_comm.move_f_meters"
)
