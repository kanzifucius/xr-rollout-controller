# Releases

A maintainer cuts a release by pushing an annotated semver tag. Do not create the GitHub Release in the UI first; the Release workflow creates it.

```bash
git checkout main && git pull
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

A pre-release uses the same flow:

```bash
git tag -a v1.2.3-rc.1 -m "v1.2.3-rc.1"
git push origin v1.2.3-rc.1
```

The tag must match `vMAJOR.MINOR.PATCH`, with an optional pre-release suffix and optional build metadata (`v1.2.3-rc.1`, `v1.2.3-alpha.1`, `v1.2.3-rc.1+exp.1`). Build metadata on its own (`v1.2.3+sha`) is still a stable release. A hyphen only inside the build metadata does not make a pre-release.

Image registries reject `+` in a tag. Publish a pre-release you want to pull without build metadata (`v1.2.3-rc.1`). When the git tag includes `+build`, the chart version and the docs version keep that suffix.

## What the tag publishes

Two workflows run on that push. [Release](https://github.com/kanzifucius/xr-rollout-controller/blob/main/.github/workflows/release.yml) publishes the artefacts. [docs](https://github.com/kanzifucius/xr-rollout-controller/blob/main/.github/workflows/docs.yml) publishes the site. They do not call each other. Both ask `hack/release-tag.sh` whether the tag is stable or a pre-release.

| Artefact | Where | Name |
|----------|--------|------|
| Controller image | `ghcr.io/kanzifucius/xr-rollout-controller` | the git tag. The `latest` image tag is published from `main` only. |
| Helm chart | `oci://ghcr.io/kanzifucius/charts/xr-rollout-controller` | tag without `v` (`1.2.3` or `1.2.3-rc.1`). `appVersion` is the git tag. A pre-release stays on that version. It is not copied onto a stable version or `latest`. |
| GitHub Release | this repository | `install.yaml` and `compositionrollouts.crd.yaml`, with generated notes. A pre-release is created with `--prerelease` and `--latest=false`. |
| Docs version | GitHub Pages (mike tree on `gh-pages`) | tag without `v`. Stable tags also move the `latest` alias. Pre-releases leave `latest` on the last stable version. |

Pushes to `main` publish the image as the short SHA and as `latest`. They do not push a Helm chart. They do publish docs version `dev`.

## How a release shows up in the docs

The site is built with Material for MkDocs and deployed with [mike](https://github.com/jimporter/mike) onto the `gh-pages` branch. GitHub Pages serves that branch (folder `/`). There is no custom domain. The site URL is [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/).

| Push | mike version | Alias |
|------|----------------|-------|
| `main` | `dev` | `dev` is the default only until the first release sets one |
| tag `v1.2.3` | `1.2.3` | `latest` is updated, and `latest` becomes the default |
| tag `v1.2.3+sha` | `1.2.3+sha` | `latest` is updated (build metadata is not a pre-release) |
| tag `v1.3.0-rc.1` | `1.3.0-rc.1` | `latest` stays on the last stable version |
| tag `v1.3.0-alpha.1` | `1.3.0-alpha.1` | `latest` stays on the last stable version |

After `v1.2.3` is pushed, the version dropdown lists `1.2.3` and `latest` points at those pages. The next stable tag moves `latest`. A later pre-release publishes its own version beside that alias. Older versions stay addressable, for example `/1.2.3/` beside `/latest/`, `/1.3.0-rc.1/`, and `/dev/`.

The first successful docs deploy on `main` creates `gh-pages`. Pages must be set once to **Deploy from a branch**, branch `gh-pages`, folder `/` (root). The workflow does not use the `github-pages` environment: that environment only allows deployments from `gh-pages`, and this job runs on `main`.
