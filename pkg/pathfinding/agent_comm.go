package pathfinding

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/internal/app"
	"github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
	"github.com/nats-io/nats.go"
)

// AgentCommunicator controls and observes one running agent pathfinding job.
// It is tied to the NATS connection used to start the job.
type AgentCommunicator struct {
	nc *nats.Conn
	id string
}

// UpdateMovementSpeed changes the agent's continuous movement speed in cells
// per second. A value of zero pauses automatic movement without terminating the job.
func (c *AgentCommunicator) UpdateMovementSpeed(ctx context.Context, newTilesPerSecond float64) error {

	data, err := json.Marshal(app.FloatParams{Id: c.id, Float: newTilesPerSecond})
	if err != nil {
		return err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommUpdateMovementSpeed,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommUpdateMovementSpeed, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommUpdateMovementSpeed, domain.ErrNATSResponse, err)
	}
	return nil
}

// BlockingWait returns a channel that closes when the agent pathfinding stops.
// If the agent already stopped and its completed result is still retained, the
// returned channel closes immediately.
func (c *AgentCommunicator) BlockingWait() (<-chan struct{}, error) {
	done := make(chan struct{})

	data, err := json.Marshal(app.IdentifierParams{Id: c.id})
	if err != nil {
		return nil, err
	}

	replySubj := c.nc.NewRespInbox()

	sub, err := c.nc.Subscribe(replySubj, func(msg *nats.Msg) {
		close(done)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommBlockingWait, domain.ErrNATSSubscription, err)
	}
	sub.AutoUnsubscribe(1)

	err = c.nc.PublishRequest(subjects.AgentCommBlockingWait, replySubj, data)
	if err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommBlockingWait, domain.ErrNATSPublish, err)
	}

	return done, nil
}

// IsMoving reports whether the agent pathfinding is still running. A retained
// completed result is reported as not moving until it expires.
func (c *AgentCommunicator) IsMoving(ctx context.Context) (bool, error) {
	data, err := json.Marshal(app.IdentifierParams{Id: c.id})
	if err != nil {
		return true, err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommIsMoving,
		Data:    data,
	})
	if err != nil {
		return true, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommIsMoving, domain.ErrNATSRequest, err)
	}

	boolVal, err := messages.Data[bool](resp.Data)
	if err != nil {
		return true, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommIsMoving, domain.ErrNATSResponse, err)
	}

	return boolVal, nil
}

// ExitError returns nil when the agent reached its goal. If the agent stopped
// early, the returned error explains the terminal reason. Completed results are
// retained briefly after the agent stops; querying after that retention window
// or after a same-agent restart may not return the previous run's reason.
func (c *AgentCommunicator) ExitError(ctx context.Context) error {
	data, err := json.Marshal(app.IdentifierParams{Id: c.id})
	if err != nil {
		return err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommExitError,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommExitError, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommExitError, domain.ErrNATSResponse, err)
	}
	return nil
}

// Terminate cancels the pathfinding job and marks it as terminated.
func (c *AgentCommunicator) Terminate(ctx context.Context) error {
	data, err := json.Marshal(app.IdentifierParams{Id: c.id})
	if err != nil {
		return err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommTerminate,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommTerminate, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommTerminate, domain.ErrNATSResponse, err)
	}
	return nil
}

// Stop pauses automatic movement by setting the current speed to zero.
func (c *AgentCommunicator) Stop(ctx context.Context) error {
	data, err := json.Marshal(app.IdentifierParams{Id: c.id})
	if err != nil {
		return err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommStop,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommStop, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommStop, domain.ErrNATSResponse, err)
	}
	return nil
}

// MoveNCells asks the running pathfinder to advance up to n grid cells.
func (c *AgentCommunicator) MoveNCells(ctx context.Context, n uint) error {
	data, err := json.Marshal(app.MoveNCellsParams{Id: c.id, N: n})
	if err != nil {
		return err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommMoveNCells,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommMoveNCells, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrAgentCommMoveNCells, domain.ErrNATSResponse, err)
	}
	return nil
}

// MoveFMeters returns domain.ErrAgentExitedWithMetersLeft if the pathfinding
// stopped before consuming the requested meters, including when the agent has
// already stopped but its completed result is still retained. Calling this function
// blocks until the given meters are consumed or the agent stops.
func (c *AgentCommunicator) MoveFMeters(ctx context.Context, f float64) (remainingMeters float64, err error) {
	data, err := json.Marshal(app.FloatParams{Id: c.id, Float: f})
	if err != nil {
		return f, err
	}

	resp, err := c.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.AgentCommMoveFMeters,
		Data:    data,
	})
	if err != nil {
		return f, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommMoveFMeters, domain.ErrNATSRequest, err)
	}

	remainingMeters, err = messages.Data[float64](resp.Data)
	if err != nil {
		return f, fmt.Errorf("%w: %w: %w", domain.ErrAgentCommMoveFMeters, domain.ErrNATSResponse, err)
	}

	return remainingMeters, nil
}
