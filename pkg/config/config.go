package config

import (
	"github.com/Kentalives/LifeRouter/pkg/domain"
)

// ConfigPortal links two grid cells across floors. Coordinates are
// expressed in meters and converted to grid cells using GridConfig.CellSizeM.
type ConfigPortal struct {
	// From is the source position in meters, ordered as [x, y].
	From [2]float64 `mapstructure:"from,omitempty"`

	// FromLayer is the source pathfinding layer index.
	FromLayer int `mapstructure:"fromlayer,omitempty"`

	// To is the destination position in meters, ordered as [x, y].
	To [2]float64 `mapstructure:"to,omitempty"`

	// ToLayer is the destination pathfinding layer index.
	ToLayer int `mapstructure:"tolayer,omitempty"`

	// TraversalCost is the extra cost paid to cross the portal.
	TraversalCost domain.Cost `mapstructure:"traversalcost,omitempty"`

	// Bidirectional adds the reverse portal as well as the forward portal.
	Bidirectional bool `mapstructure:"bidirectional,omitempty"`
}

// ConfigLayer describes one navigable floor and the image used to rasterize its
// base traversability grid.
type ConfigLayer struct {
	// Name is the external floor or region name used by geotruth.
	Name string `mapstructure:"name,omitempty"`

	// ImgPath is the raster image path for this floor.
	ImgPath string `mapstructure:"img,omitempty"`
}

// AppConfig contains process-level settings for NATS connectivity and logging.
type AppConfig struct {
	// NatsServerUrl is the NATS server URL used by the service.
	NatsServerUrl string `mapstructure:"serverurl,omitempty"`

	// LogFileout enables writing logs to Logfile.
	LogFileout bool `mapstructure:"fileout,omitempty"`

	// Logfile is the file path used when LogFileout is true.
	Logfile string `mapstructure:"logfile,omitempty"`

	// LogStderr enables writing logs to standard error.
	LogStderr bool `mapstructure:"stderr,omitempty"`
}

// GridConfig defines the rasterized world dimensions and conversion units.
// Rows and Cols are grid cells, PxPerCell maps source images to cells, and
// CellSizeM maps cells to real-world meters.
type GridConfig struct {
	// FloorLayers are the rasterized floors that make up the world.
	FloorLayers []ConfigLayer `mapstructure:"layers,omitempty"`

	// Portals are cross-floor edges applied after rasterizing the layers.
	Portals []ConfigPortal `mapstructure:"portals,omitempty"`

	// Rows and Cols are the grid dimensions in cells for each configured layer.
	Rows uint `mapstructure:"rows,omitempty"`
	Cols uint `mapstructure:"cols,omitempty"`

	// PxPerCell is the number of image pixels represented by one grid cell.
	PxPerCell float64 `mapstructure:"pxpercell,omitempty"`

	// CellSizeM is the real-world side length, in meters, of one grid cell.
	CellSizeM float64 `mapstructure:"cellsizem,omitempty"`

	// WallAuraCells is the Chebyshev radius around walls that receives extra cost.
	WallAuraCells int `mapstructure:"wallauracells,omitempty"`
}

// EmergencyConfig tunes global emergency flow-field updates and signaling.
type EmergencyConfig struct {
	// LightingHystheresis is the minimum cost improvement required to change a signal.
	LightingHystheresis domain.Cost `mapstructure:"lighthystheresis,omitempty"`

	// PriorityPathCellsWidth is the preferred-route width in cells.
	PriorityPathCellsWidth float64 `mapstructure:"priopathwidth,omitempty"`

	// Debug enables verbose emergency pathfinding output.
	Debug bool `mapstructure:"debug,omitempty"`
}

// AgentConfig tunes per-agent local replanning and movement behavior.
type AgentConfig struct {
	// VisionRadiusCells is the local radius an agent refreshes from geotruth.
	VisionRadiusCells uint `mapstructure:"visionradiuscells,omitempty"`

	// CellsForRealUpdate controls how often movement is published externally.
	CellsForRealUpdate int `mapstructure:"cellsforrealupdate,omitempty"`

	// MaxReusableWorldLinkedFloors limits pooled virtual-world reuse by linked floors.
	MaxReusableWorldLinkedFloors uint `mapstructure:"maxreusableworldlinkedfloors,omitempty"`

	// SparseValuePageDirectory enables sparse D* Lite value-page allocation.
	SparseValuePageDirectory bool `mapstructure:"sparsevaluepagedirectory,omitempty"`

	// Handler worker limits bound NATS callback goroutines for requests that
	// can block while an agent pathfinding job runs.
	FindPathHandlerWorkers     int `mapstructure:"findpathhandlerworkers,omitempty"`
	BlockingWaitHandlerWorkers int `mapstructure:"blockingwaithandlerworkers,omitempty"`
	MoveFMetersHandlerWorkers  int `mapstructure:"movefmetershandlerworkers,omitempty"`

	// Debug enables verbose agent pathfinding output.
	Debug bool `mapstructure:"debug,omitempty"`
}

// GeotruthConfig tunes the NATS clients used for geotruth read/write traffic.
type GeotruthConfig struct {
	// QueryConnections is the number of NATS connections used for geotruth queries.
	QueryConnections int `mapstructure:"queryconnections,omitempty"`

	// PublishConnections is the number of NATS connections used for geotruth publishes.
	PublishConnections int `mapstructure:"publishconnections,omitempty"`
}

// PathfindingConfig groups shared, emergency, and agent pathfinding settings.
type PathfindingConfig struct {
	// Emergency tunes global emergency flow-field routing.
	Emergency EmergencyConfig `mapstructure:"emergency,omitempty"`

	// Agent tunes per-agent adaptive routing.
	Agent AgentConfig `mapstructure:"agent,omitempty"`

	// Geotruth tunes the internal geotruth NATS clients used by pathfinding.
	Geotruth GeotruthConfig `mapstructure:"geotruth,omitempty"`

	// FreeMovementHeight is the z-distance in meters treated as same-floor movement.
	FreeMovementHeight float64 `mapstructure:"freemoveheight,omitempty"`
}

// Config is the complete runtime configuration consumed by the service.
type Config struct {
	// App contains process-level settings.
	App AppConfig `mapstructure:"app,omitempty"`

	// Grid contains rasterized world settings.
	Grid GridConfig `mapstructure:"grid,omitempty"`

	// Pathfinding contains algorithm and handler tuning settings.
	Pathfinding PathfindingConfig `mapstructure:"pathfinding,omitempty"`
}

// Dependencies inject the external systems used for geospatial queries,
// publishing object positions, and domain-specific environment data.
type Dependencies struct {
	// Ex provides domain-specific traversal and signaling behavior.
	Ex domain.ExternalSystem

	// Qu reads geospatial object and region data.
	Qu domain.GeoQuery

	// Pu publishes object movement updates.
	Pu domain.GeoPublish
}
