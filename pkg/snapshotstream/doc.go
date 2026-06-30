// Package snapshotstream encodes floor-keyed uint8 grid snapshots for NATS
// streams. It preserves the legacy small {cells: ...} message shape and chunks
// large snapshots as gzip-compressed JSON.
package snapshotstream
