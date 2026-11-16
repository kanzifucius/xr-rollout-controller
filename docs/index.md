---
search:
  boost: 2
---

# XR Rollout Controller

The controller rolls Crossplane v2 Manual composite resources (XRs) onto a new CompositionRevision: canary, soak, an environment gate, then waves. The only field it writes is `spec.crossplane.compositionRevisionRef`. A failure pauses the rollout. Already-moved XRs stay on the new revision.

Pick a mode before you install. `local` is the default. `remote` and `hub` use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. That module is not a GA multi-cluster API, so kubeconfig Secrets and the hub layout can change. `local` does not use it.

| Mode | Pick it when | What this install does |
|------|----------------|------------------------|
| `local` | The controller and the XRs are on one cluster. Also the install on each spoke for `remote`. | Rolls XRs on this cluster. |
| `remote` | Each cluster should roll its own XRs, and one management install should open the next cluster. | Appends `Start` only. Each spoke runs `local`. |
| `hub` | `CompositionRollout` objects and their status should stay on the management cluster, with no controller on the spokes. | Patches `compositionRevisionRef` on spoke XRs. A rollout can name its own kubeconfig Secret. Hub mode only. |

Set `spec.dryRun: true` to simulate that rollout without writing the field. See [Dry run](dry-run.md).

## What it does

1. Watch a cluster-scoped `CompositionRollout` and new `CompositionRevision` objects for the compositions it names.
2. Canary a frozen subset (label, count, or percent) and patch those XRs first.
3. Soak until the canaries hold `Ready=True` and `Synced=True` for the configured duration.
4. Gate the waves with `AutoPromote` or `ManualApproval`. In `local`, and on each `remote` spoke, the key is that pod's `--environment`. In `hub`, the key is the annotation `rollouts.platform.example.org/environment` on the kubeconfig Secret.
5. Wave the remainder in bounded batches.

## Where to start

<div class="doc-map" markdown="1">

<div markdown="1">

**[Local mode](getting-started/local.md)**  
Default. Controller and XRs on one cluster. The spoke install for remote.

</div>

<div markdown="1">

**[Remote mode](getting-started/remote.md)**  
Alpha. Spokes roll their own XRs. This install only appends Start.

</div>

<div markdown="1">

**[Hub mode](getting-started/hub.md)**  
Alpha. Objects stay here. This install patches spoke XRs. No controller on the spokes.

</div>

<div markdown="1">

**[Install](install.md)**  
Public GHCR image and OCI Helm chart.

</div>

<div markdown="1">

**[CLI](cli.md)**  
`xr-rollout` on the current context. The bare command is the terminal UI.

</div>

<div markdown="1">

**[CompositionRollout](crd.md)**  
Spec, status, and a sample manifest.

</div>

<div markdown="1">

**[Canary, soak, and waves](canary.md)**  
Selection, soak, waves, and pause.

</div>

<div markdown="1">

**[Environment gate](environment.md)**  
`--environment` on a spoke. The Secret annotation in hub.

</div>

<div markdown="1">

**[Triggers](triggers.md)**  
Start, Approve, Resume, and Abort.

</div>

<div markdown="1">

**[Architecture](architecture/index.md)**  
Runtime, rollout flow, and the Approve sequence.

</div>

<div markdown="1">

**[Releases](releases.md)**  
Image, chart, and how a tag becomes a docs version.

</div>

<div markdown="1">

**[Contributing](contributing.md)**  
Local `mkdocs build` and tests.

</div>

</div>

The longer design note stays in the repository: [`docs/xr-rollout-controller-plan.md`](https://github.com/kanzifucius/xr-rollout-controller/blob/main/docs/xr-rollout-controller-plan.md).

## Site features

- **Search** — Material’s built-in client-side search. Use the search box in the header.
- **Versions** — published with [mike](https://github.com/jimporter/mike). Use the version selector in the header.

| Version / alias | Meaning |
|-----------------|---------|
| `dev` | Docs from the tip of `main`. The site root redirects here until a stable release publishes `latest`. |
| `0.1.0-rc.5` | Docs for pre-release tag `v0.1.0-rc.5`. A pre-release does not become `latest`. |
| `latest` | Not published yet. It will track the newest stable tag once one exists. |

The site root redirects to [dev](https://kanzifucius.github.io/xr-rollout-controller/dev/).
