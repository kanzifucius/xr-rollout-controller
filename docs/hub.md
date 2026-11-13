# Hub

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Engagement, kubeconfig Secrets, and the hub layout can change. Local mode does not use it.

`--mode=hub` keeps every `CompositionRollout` and its status on this install. One object names one cluster through `spec.cluster`. The process lists Compositions, CompositionRevisions, and composite resources (XRs) on that spoke, and it server-side applies `spec.crossplane.compositionRevisionRef.name` on Manual XRs. Field manager `xr-rollout-controller`.

Setup is in [Getting started: hub](getting-started/hub.md).

## Cluster and environment

`spec.cluster` is the kubeconfig Secret name, such as `acc`. The annotation `rollouts.platform.example.org/environment` is the promotion key, such as `acceptance`. The pod `--environment` flag is not read for these clusters.

A missing or empty annotation stops that reconcile before the cluster client is used. No XR patch, no status write. A Secret whose kubeconfig bytes do not change can still pick up a new annotation.

If `spec.promotion.environments` has no entry for that key, `defaultPolicy` applies. It defaults to `ManualApproval`.

## Status, dry run, and Start

Read health on the hub object: phase, pause reason, canary counts. Events are recorded on that object.

`spec.dryRun: true` does not patch XRs, does not write `compositionUpdatePolicy`, and does not publish phase `Completed`. Abort and pause do not move XRs back.

The promoter still only appends `Start`, and it appends it on the hub object whose `spec.cluster` is the next cluster. `Paused` and `Aborted` do not promote.

The metrics rollout label is `<spec.cluster>/<object name>`, for example `acc/team-payments`. There is no per-XR label. `--metrics-aggregate-only` drops the composition label and keeps the cluster prefix, for example `acc/_all`.
