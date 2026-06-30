package pathfinding

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/midtxwn/geotruth/pkg/messages"

	"github.com/Kentalives/LifeRouter/internal/app"
	"github.com/Kentalives/LifeRouter/pkg/domain"
	"github.com/Kentalives/LifeRouter/pkg/snapshotstream"
	"github.com/Kentalives/LifeRouter/pkg/subjects"
	"github.com/nats-io/nats.go"
)

// Pathfinding is a NATS-backed client for agent route-planning requests. It
// does not own the connection; callers are responsible for the NATS lifecycle.
type Pathfinding struct {
	nc *nats.Conn
}

// New creates a pathfinding client using an existing NATS connection.
func New(nc *nats.Conn) Pathfinding {
	return Pathfinding{nc: nc}
}

// AgentFindPath starts adaptive pathfinding for an existing geotruth object.
// goal is ordered as [x, y, z] in meters; movement speed is in cells/second.
func (p Pathfinding) AgentFindPath(ctx context.Context, goal [3]float64, agentId string, defaultCellsPerSecondMovement float64, moveNSteps uint) (*AgentCommunicator, error) {

	data, err := json.Marshal(app.AgentFindPathParams{Goal: goal, AgentId: agentId, DefaultCellsPerSecondMovement: defaultCellsPerSecondMovement, MoveNSteps: moveNSteps})
	if err != nil {
		return nil, err
	}

	resp, err := p.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.PathfindingAgentFindPath,
		Data:    data,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentFindPath, domain.ErrNATSRequest, err)
	}

	id, err := messages.Data[string](resp.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentFindPath, domain.ErrNATSResponse, err)
	}

	comm := &AgentCommunicator{id: id, nc: p.nc}
	return comm, nil
}

// AgentNaivePathCost computes the current path cost without starting movement.
// goal is ordered as [x, y, z] in meters and agentId must exist in geotruth.
func (p Pathfinding) AgentNaivePathCost(ctx context.Context, goal [3]float64, agentId string) (domain.Cost, error) {

	data, err := json.Marshal(app.AgentNaivePathCostParams{Goal: goal, AgentId: agentId})
	if err != nil {
		return 0, err
	}

	resp, err := p.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.PathfindingAgentNaivePathCost,
		Data:    data,
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %w: %w", domain.ErrAgentNaivePathCost, domain.ErrNATSRequest, err)
	}

	cost, err := messages.Data[domain.Cost](resp.Data)
	if err != nil {
		return 0, fmt.Errorf("%w: %w: %w", domain.ErrAgentNaivePathCost, domain.ErrNATSResponse, err)
	}

	return cost, nil
}

// AgentPathSub subscribes to path visualization snapshots for a running agent.
// Setup errors are returned before the channel is exposed. The returned channel
// closes when the service reports that the subscription is done.
func (p Pathfinding) AgentPathSub(ctx context.Context, id string) (<-chan map[string][]domain.CellState, error) {

	dataSender := make(chan map[string][]domain.CellState)

	data, err := json.Marshal(app.IdentifierParams{Id: id})
	if err != nil {
		return nil, err
	}

	replySubj := p.nc.NewRespInbox()
	msgCh := make(chan *nats.Msg, 16)
	sub, err := p.nc.ChanSubscribe(replySubj, msgCh)
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentPathSub, domain.ErrNATSSubscription, err)
	}

	err = p.nc.PublishRequest(subjects.PathfindingAgentWatchPath, replySubj, data)
	if err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentPathSub, domain.ErrNATSPublish, err)
	}

	select {
	case msg := <-msgCh:
		if err := messages.Err(msg.Data); err != nil {
			sub.Unsubscribe()
			return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentPathSub, domain.ErrNATSResponse, err)
		}
	case <-ctx.Done():
		sub.Unsubscribe()
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrAgentPathSub, domain.ErrNATSResponse, ctx.Err())
	}

	go func() {
		defer sub.Unsubscribe()
		var assembler snapshotstream.Assembler
		for msg := range msgCh {
			var resp snapshotstream.Envelope
			if err := json.Unmarshal(msg.Data, &resp); err != nil {
				log.Printf("-agent path sub err- %s", err)
				continue
			}
			if resp.Done {
				close(dataSender)
				return
			}

			if resp.Encoding != "" {
				state, ready, err := assembler.Add(resp)
				if err != nil {
					log.Printf("-agent path sub chunk err- %s", err)
					continue
				}
				if ready {
					dataSender <- state
				}
				continue
			}

			if resp.Cells != nil {
				dataSender <- resp.Cells
			}
		}
	}()

	return dataSender, nil
}
