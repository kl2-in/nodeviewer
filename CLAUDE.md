# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
make build          # compile → ./bin/knv
make mock           # build + run with mock data (no cluster needed)
make run            # build + run against current kubeconfig context
go test ./...       # run all tests
go test ./model/... # run tests for a single package
go test -run TestFoo ./model/  # run a single test by name
go vet ./...        # lint
gofmt -l .          # check formatting (CI rejects unformatted files)
```

## Architecture

The app is a [bubbletea](https://github.com/charmbracelet/bubbletea) TUI. Data flows in one direction:

```
k8s.Client → model.Loader interface → model.Model (bubbletea) → ui (lipgloss)
```

**`main.go`** — parses flags, builds a `k8s.Client` or `model.MockLoader`, starts the informers (`client.Start`), then hands a `model.Config` to `tea.NewProgram`.

**`k8s/`** — the only package that touches the Kubernetes API.
- `client.go`: constructs the clientset, dynamic client, shared informer factories (core + dynamic for Karpenter NodeClaims), and listers. `Start()` launches the informers and blocks until the cache is synced (30s timeout). Listers use `labels.Everything()`, not `metav1.ListOptions`.
- `fetch.go`: `FetchNodes()` reads from the lister caches (nodes, pods) and hits the API directly only for events and metrics. NodeClaim disruption status is read from the dynamic lister.
- `mutate.go`: `CordonNode`, `UncordonNode`, `DrainNode` — uses `k8s.io/kubectl/pkg/drain` for drain.

**`model/`** — no Kubernetes imports; depends only on bubbletea and the `ui` package.
- `loader.go`: `Loader` (FetchNodes) and `Mutator` (cordon/uncordon/drain) interfaces. `k8s.Client` implements both; `MockLoader` implements only `Loader`. The `Mutator` capability is detected via a runtime cast: `loader.(Mutator)`.
- `model.go`: the bubbletea `Model`. State machine with `viewState` constants (`viewList`, `viewInspect`, `viewFilter`, `viewConfirm`, `viewCordonConfirm`, `viewDrainConfirm`, `viewPodList`, `viewHelp`). Auto-refresh is a self-sustaining tick loop: `nodesLoadedMsg` schedules a `tea.Tick(interval)`, which fires a `tickMsg`, which calls `fetchCmd()`.
- `node.go`: `Node`, `Pod`, `Event`, `Condition` structs. All fields have `json:` tags — the `--output json` flag serialises this struct directly, so new fields need `json:"name,omitempty"`.

**`ui/`** — pure rendering; no state.
- `table.go`: all rendering entry points (`RenderList`, `RenderInspect`, `RenderStatusBar`, overlays). Column layout is dynamic — `buildColumns()` distributes width budget across available terminal width.
- `styles.go`: all `lipgloss.Style` definitions and colours in one place.

**`nodeexec/`** — builds and execs the `kubectl debug node/<name>` command.

## Key conventions

- All k8s dependencies must stay on the **same minor version** as `k8s.io/client-go` (currently `v0.35.x`).
- No CLI framework dependencies — only `flag` from stdlib.
- `model.Node` fields with `json:` tags are part of the public `--output json` API.
- Tests cover resolver/formatter pure functions in `k8s/`, bubbletea `Update` logic in `model/`, and column/rendering helpers in `ui/`. No integration tests require a real cluster; use `MockLoader` or construct a `model.Model` directly.

## Adding a new column

1. Add field to `model.Node` in `model/node.go` (with json tag).
2. Add to `ui.NodeRow` and `buildColumns()` in `ui/table.go`.
3. Handle in `renderRow()` and populate in `model.renderList()` / `model.buildInspectLines()`.
4. If sortable, add to `sortKeyForTitle()`, `sortKeyToTitle()`, and `model.applySort()`.

## Adding a new node operation

1. Add method to `model.Mutator` in `model/loader.go`.
2. Implement on `*k8s.Client` in `k8s/mutate.go`.
3. Add no-op to `model.MockLoader` in `model/mock.go`.
4. Add `viewState` constant and confirm overlay in `model/model.go`.
5. Add key bindings in `viewList` and `viewInspect` cases.
6. Add render function in `ui/table.go`.

## Commit message style

```
<area>: short summary in present tense
```

Areas: `k8s`, `model`, `ui`, `nodeexec`, `main`, `ci`, `docs`.
