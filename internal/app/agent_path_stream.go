package app

import (
	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/snapshotstream"
)

const AgentPathSubChunkEncodingGzipJSON = snapshotstream.EncodingGzipJSON

type AgentPathSubResponse = snapshotstream.Envelope
type AgentPathSubChunkAssembler = snapshotstream.Assembler
type EmergencySubResponse = snapshotstream.Envelope

// MarshalAgentPathSubSnapshot keeps small snapshots compatible with the legacy
// {cells: ...} message and chunks large snapshots below the usual NATS payload
// limit.
func MarshalAgentPathSubSnapshot(state map[string][]pubdomain.CellState) ([][]byte, error) {
	return snapshotstream.MarshalSnapshot(state)
}

// MarshalEmergencyFlowSnapshot uses the shared snapshot stream codec for
// emergency flow directions.
func MarshalEmergencyFlowSnapshot(state map[string][]pubdomain.Direction) ([][]byte, error) {
	return snapshotstream.MarshalSnapshot(state)
}
