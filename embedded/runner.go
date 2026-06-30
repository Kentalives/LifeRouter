package embedded

import (
	"context"
	"time"

	"github.com/Kentalives/LifeRouter/internal/app"
	intconfig "github.com/Kentalives/LifeRouter/internal/config"
	"github.com/Kentalives/LifeRouter/internal/geotruthpool"
	"github.com/Kentalives/LifeRouter/internal/mock"
	"github.com/Kentalives/LifeRouter/pkg/config"
	"github.com/pkg/errors"
)

// DefaultConfig returns the same local defaults used by the standalone service.
// The values are convenient for embedded/debug runs; callers that embed the
// service in another deployment should override paths, NATS URL, and grid data.
func DefaultConfig() *config.Config {
	return intconfig.DefaultConfig()
}

// DefaultDependencies builds the default external dependencies for an embedded
// run. It still connects to NATS and uses a mock ExternalSystem, so production
// embedders should usually provide their own Dependencies instead or at least
// change the ExternalSystem for one of their own.
func DefaultDependencies(ctx context.Context, cfg *config.Config) (*config.Dependencies, error) {

	ctx2, cancel := context.WithTimeout(ctx, 20*time.Second)
	q, err := geotruthpool.NewNATSQueryPool(ctx2, cfg.App.NatsServerUrl, cfg.Pathfinding.Geotruth.QueryConnections)
	cancel()
	if err != nil {
		return nil, errors.Wrap(err, "creating geotruth query pool")
	}

	ctx2, cancel = context.WithTimeout(ctx, 20*time.Second)
	p, err := geotruthpool.NewNATSPublishPool(ctx2, cfg.App.NatsServerUrl, cfg.Pathfinding.Geotruth.PublishConnections)
	cancel()
	if err != nil {
		q.Close()
		return nil, errors.Wrap(err, "creating geotruth publish pool")
	}

	ex, err := mock.NewExternalSystem([]string{"0"}, []float64{0}, nil)
	if err != nil {
		q.Close()
		p.Close()
		return nil, errors.Wrap(err, "creating mock external system")
	}

	return &config.Dependencies{
		Ex: ex,
		Qu: q,
		Pu: p,
	}, nil
}

// Run starts the pathfinding dispatcher in the caller process using the same
// application path as the standalone service. The returned dispatcher owns the
// service subscriptions and must be shut down by the caller.
func Run(ctx context.Context, cfg *config.Config, dep *config.Dependencies) (*app.Dispatcher, error) {

	return app.Run(ctx, cfg, dep)
}
