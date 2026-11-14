# CLI

`xr-rollout` reads `CompositionRollout` and `PromotionPipeline` on the current cluster. It uses the current kube context and kubeconfig. `--context` and `--kubeconfig` select a different one. Permission is the caller's RBAC.

The CLI does not hold a spoke kubeconfig. It does not patch composite resources. A trigger is a server-side apply of one `spec.triggers` entry, field manager `xr-rollout-cli`. Other entries stay, including ones written earlier by this command.

With no subcommand, `xr-rollout` opens the terminal UI. Subcommands do not prompt.

## Install

The binary is attached to the same GitHub Release as the controller. There is no separate tag. A pre-release tag publishes the binary on that pre-release. It does not become the Latest release.

```bash
gh release download v1.2.3 --repo kanzifucius/xr-rollout-controller --pattern xr-rollout-linux-amd64
install xr-rollout-linux-amd64 "$HOME/.local/bin/xr-rollout"
ln -s "$HOME/.local/bin/xr-rollout" "$HOME/.local/bin/kubectl-xr_rollout"
```

`kubectl xr rollout` runs that symlink. The assets are:

| Asset | Machine |
|-------|---------|
| `xr-rollout-linux-amd64` | Linux x86-64 |
| `xr-rollout-linux-arm64` | Linux arm64 |
| `xr-rollout-darwin-amd64` | macOS Intel |
| `xr-rollout-darwin-arm64` | macOS Apple silicon |
| `xr-rollout-windows-amd64.exe` | Windows x86-64 |

From a checkout of this repository:

```bash
make build-cli
```

The binary is `bin/xr-rollout`. The symlink above is the same file.

## Commands

| Command | Effect |
|---------|--------|
| `xr-rollout list` | Table of rollouts on this context. |
| `xr-rollout get NAME` | One rollout: phase, pause reason, canary, revision, cluster. |
| `xr-rollout pipeline status [NAME]` | Pipeline hops. `xr-rollout pipeline` prints the same status. |
| `xr-rollout start NAME --composition NAME --revision REV` | Append `Start` on that rollout. |
| `xr-rollout approve NAME --composition NAME --revision REV` | Append `Approve` on that rollout. |
| `xr-rollout resume NAME --composition NAME --revision REV` | Append `Resume` on that rollout. |
| `xr-rollout abort NAME --composition NAME --revision REV` | Append `Abort` on that rollout. |
| `xr-rollout skip NAME --cluster NAME --revision REV` | Append `Skip` on that pipeline. |

`--actor` is optional. `skip` also takes `--rollout` and `--composition`. Leave those empty to match every rollout or composition on the cluster.

`approve` here is the in-cluster trigger on `CompositionRollout`. A cross-cluster `Approve` is a `PromotionPipeline` trigger, and this command does not write that one. `skip` is the pipeline trigger. See [Triggers](triggers.md) and [Promotion across clusters](promotion.md).

`-o json` prints JSON instead of the table. `--watch` on `list`, `get`, and `pipeline status` polls every 30 seconds.

## Terminal UI

`xr-rollout` with no arguments lists the rollouts on this context. The columns are name, composition, cluster, phase, revision, and pause reason. `j` and `k`, or the arrow keys, move. Enter opens the row.

The detail shows the phase, the pause reason, the canary, the revision, and the cluster. `s` starts, `a` approves, `r` resumes, `x` aborts, and `k` skips. The UI asks before it writes. `y` appends the trigger. `n` or Esc cancels, and nothing is written. When more than one pipeline contains the cluster, left and right choose the pipeline for Skip.

The view polls every 30 seconds. `q` quits.
