package app

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	pubdomain "github.com/Kentalives/LifeRouter/pkg/domain"
)

func TestMarshalAgentPathSubSnapshotKeepsSmallLegacyMessage(t *testing.T) {
	want := map[string][]pubdomain.CellState{"0": {pubdomain.STATE_OnPath}}
	payloads, err := MarshalAgentPathSubSnapshot(want)
	if err != nil {
		t.Fatalf("MarshalAgentPathSubSnapshot: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	var resp AgentPathSubResponse
	if err := json.Unmarshal(payloads[0], &resp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if resp.Encoding != "" {
		t.Fatalf("encoding = %q, want legacy empty encoding", resp.Encoding)
	}
	if !reflect.DeepEqual(resp.Cells, want) {
		t.Fatalf("cells = %#v, want %#v", resp.Cells, want)
	}
}
func TestMarshalAgentPathSubSnapshotChunksLargeMessage(t *testing.T) {
	want := largeAgentPathSubHeatmap(5, 403*1010)
	payloads, err := MarshalAgentPathSubSnapshot(want)
	if err != nil {
		t.Fatalf("MarshalAgentPathSubSnapshot: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("expected at least one payload")
	}
	var assembler AgentPathSubChunkAssembler
	var got map[string][]pubdomain.CellState
	for _, payload := range payloads {
		if len(payload) > 900*1024 {
			t.Fatalf("chunk payload length = %d, want below 900 KiB", len(payload))
		}
		var resp AgentPathSubResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if resp.Encoding == "" {
			t.Fatalf("large payload used legacy encoding with %d bytes", len(payload))
		}
		state, ready, err := assembler.Add(resp)
		if err != nil {
			t.Fatalf("assembler add: %v", err)
		}
		if ready {
			got = state
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("decoded large heatmap did not match original")
	}
}
func largeAgentPathSubHeatmap(floors, cellsPerFloor int) map[string][]pubdomain.CellState {
	heatmap := make(map[string][]pubdomain.CellState, floors)
	var seed uint32 = 1
	for floor := 0; floor < floors; floor++ {
		cells := make([]pubdomain.CellState, cellsPerFloor)
		for i := range cells {
			seed = seed*1664525 + 1013904223
			cells[i] = pubdomain.CellState((seed >> 16) % 6)
		}
		heatmap[strconv.Itoa(floor)] = cells
	}
	return heatmap
}
