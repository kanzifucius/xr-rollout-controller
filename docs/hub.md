# Hub

Remote and hub use [sigs.k8s.io/multicluster-runtime](https://github.com/kubernetes-sigs/multicluster-runtime) v0.20.4-alpha.7. It is not a GA multi-cluster API. Engagement, kubeconfig Secrets, and the hub layout can change. Local mode does not use it.

`--mode=hub` keeps every `CompositionRollout` and its status on this install. `spec.cluster` is the logical cluster. The process lists Compositions, CompositionRevisions, and composite resources (XRs) with the kubeconfig that object names, and it server-side applies `spec.crossplane.compositionRevisionRef.name` on Manual XRs. Field manager `xr-rollout-controller`. Several objects may share one `spec.cluster` when each governs different compositions.

Setup is in [Getting started: hub](getting-started/hub.md).

## Cluster and environment

`spec.cluster` is the logical cluster and the promotion key, such as `acc`. When `spec.kubeconfigSecretRef` is unset, the kubeconfig Secret name is `spec.cluster`. A rollout can name another Secret. That is hub mode only. Local mode has no kubeconfig Secrets. Remote mode keeps one Secret per cluster.

The Secret annotation `rollouts.platform.example.org/cluster`, when set, is the logical cluster, so several Secrets can share one `spec.cluster`. Each Secret name is its own client. The promotion pipeline does not choose the credential.

The annotation `rollouts.platform.example.org/environment` is the promotion environment, such as `acceptance`. The pod `--environment` flag is not read for these clusters.

A missing or empty environment annotation stops that reconcile before the cluster client is used. No XR patch, no status write. A Secret whose kubeconfig bytes do not change can still pick up a new annotation. A missing Secret, or a kubeconfig that will not parse, pauses that rollout only. A user that cannot patch `compositionRevisionRef` pauses that rollout only.

If `spec.promotion.environments` has no entry for that key, `defaultPolicy` applies. It defaults to `ManualApproval`.

## Status, dry run, and Start

Read health on the hub object: phase, pause reason, canary counts. Events are recorded on that object.

`spec.dryRun: true` lists with that object's client, does not patch XRs, does not write `compositionUpdatePolicy`, and does not publish phase `Completed`. A missing credential pauses the dry run and does not record `wouldUpdate`. Abort and pause do not move XRs back.

The promoter still only appends `Start`, and it appends it on the hub object whose `spec.cluster` is the next cluster. `Paused` and `Aborted` do not promote. The pipeline matches `spec.cluster`. It does not select a kubeconfig.

The metrics rollout label is `<spec.cluster>/<object name>`, for example `acc/team-payments`. There is no per-XR label. `--metrics-aggregate-only` drops the composition label and keeps the cluster prefix, for example `acc/_all`.
