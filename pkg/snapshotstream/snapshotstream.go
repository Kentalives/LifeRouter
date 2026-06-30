package snapshotstream

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
)

const (
	// EncodingGzipJSON identifies chunked gzip-compressed JSON snapshots.
	EncodingGzipJSON      = "gzip+json"
	maxLegacyPayloadBytes = 512 * 1024
	chunkDataBytes        = 512 * 1024
)

var snapshotSeq atomic.Uint64

// Snapshot is a floor-keyed grid snapshot. The values are uint8 so callers can
// reuse the stream format for domain.CellState and domain.Direction arrays.
type Snapshot = map[string][]uint8

// Envelope is the wire format used by snapshot streams. Small snapshots are
// sent as legacy {cells: ...} envelopes. Large snapshots are sent as gzip+json
// chunks with the same chunk metadata.
type Envelope struct {
	Done       bool     `json:"done"`
	Cells      Snapshot `json:"cells,omitempty"`
	Encoding   string   `json:"encoding,omitempty"`
	SnapshotID string   `json:"snapshotId,omitempty"`
	ChunkIndex int      `json:"chunkIndex,omitempty"`
	ChunkCount int      `json:"chunkCount,omitempty"`
	Data       []byte   `json:"data,omitempty"`
}

// MarshalDone returns the terminal stream envelope.
func MarshalDone() ([]byte, error) {
	return json.Marshal(Envelope{Done: true})
}

// MarshalSnapshot keeps small snapshots compatible with the legacy {cells: ...}
// message and chunks large snapshots below the usual NATS payload limit.
func MarshalSnapshot(cells Snapshot) ([][]byte, error) {
	legacy, err := json.Marshal(Envelope{Cells: cells})
	if err != nil {
		return nil, err
	}
	if len(legacy) <= maxLegacyPayloadBytes {
		return [][]byte{legacy}, nil
	}
	raw, err := json.Marshal(cells)
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(raw); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	snapshotID := strconv.FormatUint(snapshotSeq.Add(1), 10)
	data := compressed.Bytes()
	chunkCount := (len(data) + chunkDataBytes - 1) / chunkDataBytes
	payloads := make([][]byte, 0, chunkCount)
	for i := 0; i < chunkCount; i++ {
		start := i * chunkDataBytes
		end := min(start+chunkDataBytes, len(data))
		payload, err := json.Marshal(Envelope{
			Encoding:   EncodingGzipJSON,
			SnapshotID: snapshotID,
			ChunkIndex: i,
			ChunkCount: chunkCount,
			Data:       data[start:end],
		})
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

// Assembler reconstructs chunked snapshot envelopes.
type Assembler struct {
	snapshotID string
	chunkCount int
	chunks     [][]byte
	received   int
}

// Add consumes one chunk envelope. ready is true only when a complete snapshot
// has been assembled.
func (a *Assembler) Add(resp Envelope) (Snapshot, bool, error) {
	if resp.Encoding != EncodingGzipJSON {
		return nil, false, fmt.Errorf("unsupported snapshot encoding %q", resp.Encoding)
	}
	if resp.SnapshotID == "" {
		return nil, false, fmt.Errorf("missing snapshot id")
	}
	if resp.ChunkCount <= 0 {
		return nil, false, fmt.Errorf("invalid snapshot chunk count %d", resp.ChunkCount)
	}
	if resp.ChunkIndex < 0 || resp.ChunkIndex >= resp.ChunkCount {
		return nil, false, fmt.Errorf("invalid snapshot chunk index %d/%d", resp.ChunkIndex, resp.ChunkCount)
	}
	if a.snapshotID != resp.SnapshotID {
		a.snapshotID = resp.SnapshotID
		a.chunkCount = resp.ChunkCount
		a.chunks = make([][]byte, resp.ChunkCount)
		a.received = 0
	} else if a.chunkCount != resp.ChunkCount {
		return nil, false, fmt.Errorf("snapshot %s changed chunk count from %d to %d", resp.SnapshotID, a.chunkCount, resp.ChunkCount)
	}
	if a.chunks[resp.ChunkIndex] == nil {
		a.chunks[resp.ChunkIndex] = append([]byte(nil), resp.Data...)
		a.received++
	}
	if a.received != a.chunkCount {
		return nil, false, nil
	}
	var compressed bytes.Buffer
	for i, chunk := range a.chunks {
		if chunk == nil {
			return nil, false, fmt.Errorf("snapshot %s missing chunk %d", a.snapshotID, i)
		}
		compressed.Write(chunk)
	}
	gz, err := gzip.NewReader(&compressed)
	if err != nil {
		return nil, false, err
	}
	raw, err := io.ReadAll(gz)
	closeErr := gz.Close()
	if err != nil {
		return nil, false, err
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	var cells Snapshot
	if err := json.Unmarshal(raw, &cells); err != nil {
		return nil, false, err
	}
	*a = Assembler{}
	return cells, true, nil
}
