---
search:
  boost: 2
---

# XR Rollout Controller

The controller rolls a fleet of Crossplane **v2** composite resources (XRs) onto a new **CompositionRevision** in a fixed order: canary, soak, environment gate, then batched waves.

It governs XRs whose `compositionUpdatePolicy` is **Manual**. The only field it writes is `spec.crossplane.compositionRevisionRef`. Automatic XRs keep moving on their own and are counted as unmanaged.

Set `spec.dryRun: true` to simulate that rollout without writing the field. See [Dry run](dry-run.md).

## What it does

1. **Watch** a cluster-scoped `CompositionRollout` and new `CompositionRevision` objects for the compositions it names.
2. **Canary** a frozen subset (label, count, or percent) and patch those XRs first.
3. **Soak** until the canaries hold `Ready=True` and `Synced=True` for the configured duration.
4. **Gate** promotion with the policy for this cluster's `--environment` (`AutoPromote` or `ManualApproval`).
5. **Wave** the remainder in bounded batches. A failure pauses the rollout. Nothing is rolled back.

## Where to start

<div class="doc-map" markdown="1">

<div markdown="1">

**[Install](install.md)**  
Public GHCR image and OCI Helm chart.

</div>

<div markdown="1">

**[CRD](crd.md)**  
`CompositionRollout` spec, status, and a sample manifest.

</div>

<div markdown="1">

**[Canary and promotion](canary.md)**  
Selection, soak, waves, and pause.

</div>

<div markdown="1">

**[Environment gate](environment.md)**  
One manifest, per-cluster `--environment`.

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
| `latest` | Docs for the newest stable SemVer tag (default after the first release). Pre-releases do not move this alias. |
| `1.2.3` | Immutable docs for stable tag `v1.2.3` (the leading `v` is removed) |
| `1.2.3-rc.1` | Docs for a pre-release tag such as `v1.2.3-rc.1`. `latest` stays on the last stable version. |
| `dev` | Docs from the tip of `main` (may change between releases) |

Expected URL: [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/).
