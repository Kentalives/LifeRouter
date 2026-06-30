package snapshotstream

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
)

func TestMarshalSnapshotKeepsSmallLegacyMessage(t *testing.T) {
	want := Snapshot{"0": {4}}
	payloads, err := MarshalSnapshot(want)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	var resp Envelope
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
func TestMarshalSnapshotChunksLargeMessage(t *testing.T) {
	want := largeSnapshot(5, 403*1010)
	payloads, err := MarshalSnapshot(want)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("expected at least one payload")
	}
	var assembler Assembler
	var got Snapshot
	for _, payload := range payloads {
		if len(payload) > 900*1024 {
			t.Fatalf("chunk payload length = %d, want below 900 KiB", len(payload))
		}
		var resp Envelope
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
		t.Fatal("decoded large snapshot did not match original")
	}
}
func largeSnapshot(floors, cellsPerFloor int) Snapshot {
	snapshot := make(Snapshot, floors)
	var seed uint32 = 1
	for floor := 0; floor < floors; floor++ {
		cells := make([]uint8, cellsPerFloor)
		for i := range cells {
			seed = seed*1664525 + 1013904223
			cells[i] = uint8((seed >> 16) % 10)
		}
		snapshot[strconv.Itoa(floor)] = cells
	}
	return snapshot
}
