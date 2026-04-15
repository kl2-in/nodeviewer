# knv — Kubernetes Node Viewer

A fast, keyboard-driven terminal UI for monitoring and operating Kubernetes nodes.

```
ip-10-0-1-100.ec2.internal▲  Ready                       workers-prod       45d  on-demand  Eligible    m5.xlarge     us-east-1a    v1.27.3  ████████░░  62%  ████████░░  75%  18/110
ip-10-0-2-200.ec2.internal   Ready                       workers-prod       12d  spot       Disrupting  m5.2xlarge    us-east-1b    v1.27.3  ██████████  75%  ████████░░  62%  34/110
ip-10-0-3-50.ec2.internal    NotReady,SchedulingDisabled workers-spot        3d  spot       Blocked     c5.large      us-east-1c    v1.27.3  ░░░░░░░░░░   5%  ████░░░░░░  25%   2/110
ip-10-0-1-150.ec2.internal   Ready                       control-plane      60d  on-demand              m5.large      us-east-1a    v1.27.3  ████░░░░░░  38%  ████░░░░░░  37%   9/110
ip-10-0-2-175.ec2.internal   Unknown                     workers-spot        1d  spot       Drifted     t3.medium     us-east-1b    v1.27.1  ░░░░░░░░░░   0%  ░░░░░░░░░░   0%   0/110
```

## Features

- **Live cluster data** — nodes, pods, events, and metrics from any kubeconfig context
- **Inspect panel** — full node detail: conditions, taints, labels, pods, events, network, resources
- **Node operations** — exec into nodes, cordon/uncordon, drain (with PDB support)
- **Karpenter-aware** — shows real-time NodeClaim status (Consolidatable/Drifted/Disrupting/Blocked/lifecycle states)
- **Smart filtering** — substring search or `label:key=value` syntax
- **Sorting** — click column headers or press `s` to sort by any column
- **Mouse support** — click headers to sort, click rows to select
- **Viewport scrolling** — handles clusters of any size
- **Non-TUI mode** — `--output json|table` for scripting and CI
- **NO_COLOR** support — honours the [no-color.org](https://no-color.org) convention

## Installation

### Homebrew (macOS / Linux)

```sh
brew install kl2-in/tap/knv
```

### Pre-built binary

Download from the [Releases](https://github.com/kl2-in/nodeviewer/releases) page and place `knv` in your `$PATH`.

### Build from source

Requires Go 1.21+.

```sh
git clone https://github.com/kl2-in/nodeviewer.git
cd nodeviewer
make install        # builds ./bin/knv and copies to /usr/local/bin
```

Or just build without installing:

```sh
make build          # → ./bin/knv
```

## Usage

```sh
# Connect to current kubeconfig context
knv

# Use a specific context
knv -context staging-us-east-1

# Use a specific kubeconfig file
knv -kubeconfig /etc/kubeconfigs/prod.yaml

# Faster refresh (5s minimum)
knv -refresh 5

# Run with mock data (no cluster needed)
knv -mock
```

### Non-TUI output

```sh
# JSON — pipe to jq, store in CI artefacts, etc.
knv -output json | jq '.[] | select(.cpuPercent > 80)'

# Plain table — for terminal use without colour
knv -output table
```

### All flags

| Flag | Default | Description |
|---|---|---|
| `-context` | current-context | Kubernetes context name |
| `-kubeconfig` | `$KUBECONFIG` or `~/.kube/config` | Path to kubeconfig file |
| `-refresh` | `30` | Refresh interval in seconds (min 5) |
| `-output` | — | Non-TUI mode: `json` or `table` |
| `-debug-image` | `nicolaka/netshoot` | Image for `kubectl debug` node sessions |
| `-debug-namespace` | server default | Namespace for debug pods |
| `-no-color` | false | Disable ANSI colour output |
| `-debug` | — | Write debug log to file path |
| `-mock` | false | Use built-in mock data instead of a live cluster |

`NO_COLOR` environment variable is also honoured.

## Keybindings

### Node List

| Key | Action |
|---|---|
| `↑` / `k`, `↓` / `j` | Navigate nodes |
| `g` / `G` | Jump to first / last |
| `enter` / `space` | Open inspect panel |
| `P` | Full pod list for selected node |
| `e` | Exec into node (`kubectl debug`) |
| `c` | Cordon / uncordon node |
| `d` | Drain node |
| `y` | Copy node name to clipboard |
| `/` | Open filter (supports `label:key=value`) |
| `esc` | Clear filter |
| `s` | Cycle sort column |
| `S` | Toggle sort direction |
| Click header | Sort by that column |
| `r` | Manual refresh |
| `?` | Toggle help overlay |
| `q` / `ctrl+c` | Quit |

### Inspect Panel

| Key | Action |
|---|---|
| `↑` / `k`, `↓` / `j` | Scroll |
| `n` / `]`, `p` / `[` | Next / previous node |
| `P` | Full pod list for this node |
| `e` | Exec into node |
| `c` | Cordon / uncordon |
| `d` | Drain |
| `y` | Copy node name |
| `b` / `esc` | Back to list |

### Filtering

Plain text filters by name, status, nodegroup, zone, instance type, arch, and OS image.

Label filter syntax:

```
/label:spot=true          # nodes where label spot=true
/label:team               # nodes that have the team label (any value)
/label:eks.amazonaws.com/nodegroup=workers-prod
```

## Node Operations

### Exec into node

Press `e` on any node to open an interactive debug session via `kubectl debug node/<name>`. The default image is `nicolaka/netshoot` (configurable with `-debug-image`). The session gets full access to the node's filesystem, processes, and network via host namespaces.

Requires `kubectl` in `$PATH` and appropriate RBAC permissions.

### Cordon / Uncordon

Press `c` to cordon (mark unschedulable) or uncordon a node. Uses the Kubernetes API directly — no `kubectl` required.

### Drain

Press `d` to drain a node. Uses the official [`k8s.io/kubectl/pkg/drain`](https://pkg.go.dev/k8s.io/kubectl/pkg/drain) library:
- DaemonSet pods are ignored
- PodDisruptionBudgets are respected
- Bare pods without a controller are **not** force-deleted

## Karpenter Integration

`knv` reads NodeClaim status directly from the Karpenter CRD (no fallbacks needed) and shows the most recent condition in the `DISRUPTION` column. The displayed state is determined by the NodeClaim condition with the latest `lastTransitionTime`:

| State | Meaning |
|---|---|
| `Consolidatable` | Node is a candidate for consolidation |
| `Drifted` | Node has drifted from its NodePool spec (e.g. `AMIDrift`) — reason shown in inspect panel |
| `Disrupting` | Disruption actively in progress |
| `Blocked` | `karpenter.sh/do-not-disrupt` annotation is set on the NodeClaim |
| `Initialized` | Node has completed initialization |
| `Registered` | Node has registered with the cluster |
| `Launched` | Node instance has been launched |
| `Ready` | NodeClaim is fully ready |
| `Eligible` | Karpenter-managed but no recognized condition found |
| `-` | Node is not managed by Karpenter |

The drift reason (e.g. `AMIDrift`, `RequirementsDrifted`) is visible in the inspect panel under **Disruption**.

## Requirements

- Go 1.21+ (build only)
- A kubeconfig with access to your cluster
- `kubectl` in `$PATH` for node exec (`e` key) — not required for other operations
- Metrics Server installed for CPU/memory usage data (optional — falls back gracefully)
- Clipboard utility for `y` key on Linux: `wl-copy`, `xclip`, or `xsel`

## Supported Clusters

Tested with EKS, GKE, AKS, and kubeadm clusters. Node group, capacity type, and Karpenter detection work across all major providers.

## License

MIT
