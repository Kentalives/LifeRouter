package emergency

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

// Emergency is a NATS-backed client for emergency flow-field control. It does
// not own the NATS connection passed to New.
type Emergency struct {
	nc *nats.Conn
}

// New creates an emergency pathfinding client using an existing NATS connection.
func New(nc *nats.Conn) Emergency {
	return Emergency{nc: nc}
}

// Start begins emergency flow-field updates toward the given goals. Each goal is
// ordered as [x, y, z] in meters; graphs optionally bias routes per floor.
func (p Emergency) Start(ctx context.Context, goals [][3]float64, graphs []domain.RouteGraph, updateTicksPerSecond float64) error {

	data, err := json.Marshal(app.EmergencyPathParams{Goals: goals, PreferenceGraph: graphs, Updatetickspersecond: updateTicksPerSecond})
	if err != nil {
		return err
	}

	resp, err := p.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.EmergencyStart,
		Data:    data,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrEmergencyStart, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrEmergencyStart, domain.ErrNATSResponse, err)
	}
	return nil
}

// Stop stops the current emergency flow-field run and removes preference paths.
func (p Emergency) Stop(ctx context.Context) error {

	resp, err := p.nc.RequestMsgWithContext(ctx, &nats.Msg{
		Subject: subjects.EmergencyStop,
		Data:    nil,
	})
	if err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrEmergencyStop, domain.ErrNATSRequest, err)
	}

	if err = messages.Err(resp.Data); err != nil {
		return fmt.Errorf("%w: %w: %w", domain.ErrEmergencyStop, domain.ErrNATSResponse, err)
	}
	return nil
}

// FlowSub subscribes to emergency flow snapshots keyed by floor name. The
// returned channel closes when the service reports the flow stream is done.
func (p Emergency) FlowSub(ctx context.Context) (<-chan map[string][]domain.Direction, error) {
	dataSender := make(chan map[string][]domain.Direction)

	replySubj := p.nc.NewRespInbox()
	var sub *nats.Subscription
	var assembler snapshotstream.Assembler
	sub, err := p.nc.Subscribe(replySubj, func(msg *nats.Msg) {
		var resp snapshotstream.Envelope
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			log.Printf("-emergency flow sub err- %s", err)
			return
		}

		if resp.Done {
			close(dataSender)
			sub.Unsubscribe()
			return
		}
		if resp.Encoding != "" {
			state, ready, err := assembler.Add(resp)
			if err != nil {
				log.Printf("-emergency flow sub chunk err- %s", err)
				return
			}
			if ready {
				dataSender <- state
			}
			return
		}

		if resp.Cells != nil {
			dataSender <- resp.Cells
		}
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrEmergencyFlowSub, domain.ErrNATSSubscription, err)
	}

	err = p.nc.PublishRequest(subjects.EmergencyFlowWatch, replySubj, nil)
	if err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("%w: %w: %w", domain.ErrEmergencyFlowSub, domain.ErrNATSPublish, err)
	}

	return dataSender, nil
}
