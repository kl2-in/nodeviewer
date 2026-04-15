# Contributing to knv

Thank you for taking the time to contribute.

## Getting started

```sh
git clone https://github.com/kl2-in/nodeviewer.git
cd nodeviewer
go mod download
make build      # builds ./bin/knv
make mock       # runs the TUI with built-in mock data (no cluster needed)
```

## Project structure

```
.
├── main.go              # Entry point — flags, loader setup, TUI bootstrap
├── k8s/
│   ├── client.go        # Kubernetes client construction (Options, context, kubeconfig)
│   ├── fetch.go         # FetchNodes() — node/pod/event/metrics listing
│   └── mutate.go        # CordonNode / UncordonNode / DrainNode (client-go + kubectl/drain)
├── model/
│   ├── node.go          # Node, Pod, Event, Condition types (with JSON tags)
│   ├── loader.go        # Loader and Mutator interfaces
│   ├── model.go         # bubbletea Model — Update / View / state machine
│   └── mock.go          # MockLoader with static data for development
├── nodeexec/
│   └── debug.go         # kubectl debug node/<name> command builder
└── ui/
    ├── styles.go        # All lipgloss styles and colour definitions
    ├── table.go         # All rendering: table, inspect, overlays, status bar
    └── clipboard.go     # Platform-specific clipboard copy
```

## Development workflow

### Running with mock data

```sh
make mock
```

This builds and runs the TUI without needing a real cluster. All features (cordon, drain, exec confirm, filter, sort) are exercisable with mock data.

### Running against a real cluster

```sh
make run
# or with a specific context:
./bin/knv -context my-staging-context
```

### Tests

```sh
go test ./...
```

Tests cover `k8s/` (resolvers, formatters, converters), `model/` (filter, sort, viewport, cursor), and `ui/` (column layout, sort indicators, rendering helpers). There are no integration tests that require a cluster.

When adding new logic, add a test. Pure functions (resolvers, formatters) should be fully covered. bubbletea `Update` logic can be tested by calling the method directly on a `Model`.

### Linting

```sh
go vet ./...
```

The project uses standard Go formatting (`gofmt`). Run `gofmt -l .` before submitting — CI will reject unformatted files.

## Making changes

### Adding a new column

1. Add a field to `ui.NodeRow` in `ui/table.go`.
2. Add the column to the `all` slice in `buildColumns()` with an appropriate base width.
3. Handle the column title in `renderRow()`'s switch.
4. Populate the field in `model.renderList()` and `model.buildInspectLines()`.
5. If the column is sortable, add it to `sortKeyForTitle()` / `sortKeyToTitle()` and `model.applySort()`.

### Adding a new node operation

1. Add the method to `model.Mutator` in `model/loader.go`.
2. Implement it on `*k8s.Client` in `k8s/mutate.go`.
3. Add a no-op implementation on `model.MockLoader` in `model/mock.go`.
4. Add a new `viewState` constant and confirm overlay in `model/model.go`.
5. Add a key binding in the `viewList` and `viewInspect` cases.
6. Add a render function in `ui/table.go`.

### Changing the data model

`model.Node` has JSON tags — any new field needs a `json:"fieldName"` tag. Fields that may be empty should use `json:"fieldName,omitempty"`. The `--output json` mode serialises the full `Node` struct directly.

## Submitting a pull request

1. Fork the repository and create a branch from `main`.
2. Make your changes, add tests.
3. Run `go test ./...` and `go vet ./...` — both must pass.
4. Keep commits focused; one logical change per commit.
5. Open a pull request with a clear description of what changed and why.

### Commit message style

```
<area>: short summary in present tense

Optional longer explanation. Wrap at 72 characters.
```

Examples:
```
k8s: add timeout to FetchNodes
ui: cap NAME column width at 45 chars on wide terminals
model: add label:key=value filter syntax
```

Areas: `k8s`, `model`, `ui`, `nodeexec`, `main`, `ci`, `docs`.

## Reporting issues

Please include:
- `knv --version` output (or the git SHA if built from source)
- Kubernetes version and provider (EKS, GKE, AKS, kubeadm, …)
- Karpenter version if the issue relates to the DISRUPTION column
- Steps to reproduce
- Whether the issue is reproducible with `knv -mock`

## Dependency policy

- Keep the dependency tree small. New dependencies need a clear justification.
- All k8s dependencies must stay on the same minor version as `k8s.io/client-go`.
- Do not add CLI framework dependencies (cobra, urfave/cli, etc.) — the project intentionally uses only `flag` from the standard library.
