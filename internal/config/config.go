package config

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"

	pubconfig "github.com/Kentalives/LifeRouter/pkg/config"
	"github.com/nats-io/nats.go"
	"github.com/spf13/viper"
)

// Cfg is the process-wide runtime configuration after Setup.
var Cfg *pubconfig.Config

// Dep is the process-wide dependency bundle after Setup.
var Dep *pubconfig.Dependencies

// Setup stores the process-wide configuration and dependency handles used by
// internal packages that still depend on global runtime state.
func Setup(cfg *pubconfig.Config, dep *pubconfig.Dependencies) {
	Cfg = cfg
	Dep = dep
}

// DefaultConfig is the complete fallback runtime configuration. LoadConfig
// registers these values with Viper before applying file and environment
// overrides.
func DefaultConfig() *pubconfig.Config {
	return &pubconfig.Config{
		App: pubconfig.AppConfig{
			NatsServerUrl: nats.DefaultURL,
			LogStderr:     true,
		},
		Grid: pubconfig.GridConfig{
			FloorLayers: []pubconfig.ConfigLayer{
				{
					Name:    "0",
					ImgPath: "../../data/map.png",
				},
			},
			Rows:          61,
			Cols:          114,
			PxPerCell:     10,
			Portals:       nil,
			WallAuraCells: 1,
			CellSizeM:     0.2,
		},
		Pathfinding: pubconfig.PathfindingConfig{
			FreeMovementHeight: 2,
			Emergency: pubconfig.EmergencyConfig{
				LightingHystheresis:    100,
				PriorityPathCellsWidth: 1,
				Debug:                  false,
			},
			Agent: pubconfig.AgentConfig{
				VisionRadiusCells:            10,
				CellsForRealUpdate:           5,
				MaxReusableWorldLinkedFloors: 2,
				FindPathHandlerWorkers:       runtime.GOMAXPROCS(0),
				BlockingWaitHandlerWorkers:   1024,
				MoveFMetersHandlerWorkers:    2 * runtime.GOMAXPROCS(0),
				Debug:                        false,
			},
			Geotruth: pubconfig.GeotruthConfig{
				QueryConnections:   runtime.GOMAXPROCS(0),
				PublishConnections: runtime.GOMAXPROCS(0),
			},
		},
	}
}

// LoadConfig resolves the service configuration from code defaults, a YAML
// config file, and PTHFSERVICE_* environment variables. When filePath is nil,
// it loads config/config.yaml relative to the process working directory. When
// filePath is non-nil, it loads exactly that file path instead.
func LoadConfig(filePath *string) (*pubconfig.Config, error) {
	v := viper.New()

	if filePath != nil {
		v.SetConfigFile(*filePath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("config/")
	}

	v.SetEnvPrefix("PTHFSERVICE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaultsFromStruct(v, DefaultConfig())

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg pubconfig.Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaultsFromStruct walks the public config struct and registers its values
// as Viper defaults using the same mapstructure tags used for unmarshalling.
func setDefaultsFromStruct(v *viper.Viper, cfg any) error {
	rv := reflect.ValueOf(cfg)

	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return fmt.Errorf("nil config pointer")
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", rv.Kind())
	}

	return setDefaults(v, rv, nil)
}

// setDefaults recursively converts exported struct fields into dotted Viper
// keys, keeping default paths synchronized with pkg/config tags.
func setDefaults(v *viper.Viper, rv reflect.Value, path []string) error {
	rt := rv.Type()

	for i := 0; i < rv.NumField(); i++ {
		sf := rt.Field(i)
		fv := rv.Field(i)

		// Skip unexported fields.
		if sf.PkgPath != "" {
			continue
		}

		name, squash, skip := mapstructureKey(sf)
		if skip {
			continue
		}

		nextPath := path
		if !squash {
			nextPath = append(path, name)
		}

		// Follow pointers if non-nil.
		for fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				// Nil pointer cannot provide a default value.
				// You could choose to set nil here, but usually you skip it.
				goto nextField
			}
			fv = fv.Elem()
		}

		if isLeaf(fv) {
			v.SetDefault(strings.Join(nextPath, "."), fv.Interface())
			continue
		}

		if fv.Kind() == reflect.Struct {
			if err := setDefaults(v, fv, nextPath); err != nil {
				return err
			}
			continue
		}

		// Maps, slices, arrays, interfaces, etc. are treated as leaf values.
		v.SetDefault(strings.Join(nextPath, "."), fv.Interface())

	nextField:
	}

	return nil
}

// mapstructureKey resolves the Viper key segment for one struct field.
func mapstructureKey(sf reflect.StructField) (name string, squash bool, skip bool) {
	tag := sf.Tag.Get("mapstructure")
	if tag == "-" {
		return "", false, true
	}

	parts := strings.Split(tag, ",")
	tagName := parts[0]

	for _, opt := range parts[1:] {
		if opt == "squash" {
			squash = true
		}
	}

	if tagName != "" {
		name = tagName
	} else {
		// This matches the common/simple case.
		// mapstructure matching is case-insensitive, but lower-case is fine
		// for Viper keys.
		name = strings.ToLower(sf.Name)
	}

	return name, squash, false
}

func isLeaf(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Struct:
		// Treat special structs as leaf values.
		// time.Duration is int64, so it is already a leaf.
		// time.Time is a struct and should usually be a leaf.
		if v.Type().PkgPath() == "time" && v.Type().Name() == "Time" {
			return true
		}
		return false
	default:
		return true
	}
}
