// Package embedded starts the pathfinding service inside the caller process.
//
// Embedded runs use the same dispatcher and NATS subjects as the standalone
// command. Callers own the service context and must shut down the returned
// dispatcher.
package embedded
