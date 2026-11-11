# Releases

A maintainer cuts a release by pushing an annotated semver tag. Do not create the GitHub Release in the UI first; the Release workflow creates it.

```bash
git checkout main && git pull
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

The tag must match `vMAJOR.MINOR.PATCH`, with an optional prerelease or build suffix (`v1.2.3-rc.1`).

## What the tag publishes

Two workflows run on that push. [Release](https://github.com/kanzifucius/xr-rollout-controller/blob/main/.github/workflows/release.yml) publishes the artefacts. [docs](https://github.com/kanzifucius/xr-rollout-controller/blob/main/.github/workflows/docs.yml) publishes the site. They do not call each other.

| Artefact | Where | Name |
|----------|--------|------|
| Controller image | `ghcr.io/kanzifucius/xr-rollout-controller` | `v1.2.3` (the git tag) |
| Helm chart | `oci://ghcr.io/kanzifucius/charts/xr-rollout-controller` | `1.2.3` (tag without `v`), `appVersion` `v1.2.3` |
| GitHub Release | this repository | `install.yaml` and `compositionrollouts.crd.yaml` |
| Docs version | GitHub Pages, `gh-pages` branch | `1.2.3`, and alias `latest` when the tag has no `-` |

Pushes to `main` publish the image as the short SHA and as `latest`. They do not push a Helm chart. They do publish docs version `dev`.

## How a release shows up in the docs

The site is built with Material for MkDocs and deployed with [mike](https://github.com/jimporter/mike) onto the `gh-pages` branch. The docs workflow then sets GitHub Pages to **Deploy from a branch**, branch `gh-pages`, folder `/` (root). There is no custom domain. The site URL is [https://kanzifucius.github.io/xr-rollout-controller/](https://kanzifucius.github.io/xr-rollout-controller/).

| Push | mike version | Alias |
|------|----------------|-------|
| `main` | `dev` | `dev` is the default only until the first release sets one |
| tag `v1.2.3` | `1.2.3` | `latest` is updated, and `latest` becomes the default |
| tag `v1.3.0-rc.1` | `1.3.0-rc.1` | `latest` is left where it is |

After `v1.2.3` is pushed, the version dropdown lists `1.2.3` and `latest` points at those pages. The next non-prerelease tag moves `latest`. Older versions stay addressable, for example `/1.2.3/` beside `/latest/` and `/dev/`.

The first successful docs deploy on `main` creates `gh-pages` and points Pages at it. If the workflow token cannot change that setting, set it once under **Settings → Pages → Build and deployment → Deploy from a branch → `gh-pages` / root**.
