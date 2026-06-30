# LifeRouter

`LifeRouter` is a Go service for dynamic indoor route planning in
multi-floor digital-twin environments. It builds a traversability grid from map
images, connects floors through configured portals, and exposes pathfinding
operations through NATS and typed Go clients.

The service supports two main workflows:

- Agent pathfinding: adaptive route planning for tracked agents using dynamic
  environment data.
- Emergency guidance: flow-field based routing toward one or more emergency
  goals, including optional preferred route graphs.

## Features

- Rasterized grid model for one or more floors.
- Portal links between floors with configurable traversal cost.
- Agent route planning, movement control, path watching, and path-cost queries.
- Emergency flow-field start, stop, and flow watching.
- NATS request/reply API with Go wrappers in `pkg/pathfinding` and
  `pkg/emergency`.
- Public raw-NATS subject constants under `pkg/subjects`.
- Embedded execution through the `embedded` package for tests, tools, or
  applications that want to run the dispatcher in process.
- Public configuration and domain types under `pkg/config` and `pkg/domain`.
- Map tuning helper under `pkg/tuning`.

## Requirements

- Go 1.25.3 or compatible with the module version in `go.mod`.
- A reachable NATS server.
- Geotruth-style external services or injected dependencies that provide object
  positions, floor/region data, movement publishing, object traversal costs, and
  emergency signaling.
- Raster map images matching the configured grid dimensions.

## Configuration

Runtime configuration is loaded from `config/config.yaml` by default. Missing
values fall back to the defaults in `internal/config.DefaultConfig`. Internal
tools, tests, benchmarks, or embedded callers can pass an explicit YAML path to
`internal/config.LoadConfig` when they need a different configuration file, such
as a scenario fixture under `data/`.

The main configuration groups are:

- `app`: NATS URL and logging output.
- `grid`: map layers, grid dimensions, meters-per-cell conversion, wall aura,
  and cross-floor portals.
- `pathfinding`: agent and emergency tuning parameters.

Settings can also be overridden with `PTHFSERVICE_*` environment variables. For
example, `grid.cellsizem` maps to `PTHFSERVICE_GRID_CELLSIZEM`.

## Run

Build the service:

```sh
make build
```

Run it from the repository root:

```sh
go run .
```

The executable loads `config/config.yaml` through `LoadConfig(nil)`, initializes
the rasterized world, connects to NATS, subscribes to the pathfinding subjects,
and shuts down on `SIGINT` or `SIGTERM`.

## Use From Go

The module path is:

```txt
github.com/Kentalives/LifeRouter
```

Agent pathfinding clients use an existing NATS connection:

```go
pf := pathfinding.New(nc)

comm, err := pf.AgentFindPath(ctx, [3]float64{10, 4, 0}, "Agent_1", 2.0, 0)
if err != nil {
	return err
}

done, err := comm.BlockingWait()
if err != nil {
	return err
}
<-done

return comm.ExitError(ctx)
```

Emergency flow-field clients use the same pattern:

```go
em := emergency.New(nc)

if err := em.Start(ctx, [][3]float64{{1, 1, 0}}, nil, 5); err != nil {
	return err
}
defer em.Stop(ctx)

flows, err := em.FlowSub(ctx)
if err != nil {
	return err
}

for snapshot := range flows {
	_ = snapshot
}
```

For in-process execution, use `embedded.Run` with a `*config.Config` and
`*config.Dependencies`. The default embedded dependencies are useful for local
debugging, but production embedders should provide their own external-system
implementation.

## Development

Run the test suite:

```sh
make test
```

Open the generated coverage report:

```sh
go tool cover -html=coverage.out
```

After dependency changes, refresh module files and vendored dependencies:

```sh
make deps
```

## Documentation

The full project manuals are available under `doc/`:

- User manual: [`doc/user_manual.tex`](doc/user_manual.tex) and
  [`doc/user_manual.pdf`](doc/user_manual.pdf).
- Technical manual: [`doc/technical_manual.tex`](doc/technical_manual.tex) and
  [`doc/technical_manual.pdf`](doc/technical_manual.pdf).

This README is intended as a short practical entry point, not a replacement for
those manuals.

## License

This project is licensed under the Apache License, Version 2.0.

See [LICENSE](LICENSE) for details.

## Author

Created by [Kentalives](https://github.com/Kentalives) as a final year academic project at University
of Santiago de Compostela.

## Citation

If you use this project in academic work, please cite it as:

Kentalives. LifeRouter. Final year academic project,
University of Santiago de Compostela, 2026.
