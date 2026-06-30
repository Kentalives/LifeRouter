// Package subjects defines the public raw-NATS subject names handled by the
// pathfinding service.
//
// Go callers should usually prefer the typed clients in pkg/pathfinding and
// pkg/emergency. These constants are provided for callers that build NATS
// messages directly or need to share the subject contract with non-Go clients.
package subjects
