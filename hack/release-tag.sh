#!/usr/bin/env bash
# Classify a release git tag for .github/workflows/release.yml and docs.yml.
#
# Source this file from a workflow step and call the functions. The
# pre-release versus stable decision lives here so both workflows share it
# and hack/release-tag_test.sh can check it.
#
#   source hack/release-tag.sh
#   classify_release_tag "${GITHUB_REF_NAME}"
#   release_image_tag_list "${RELEASE_IMAGE_REPOSITORY}"
#   release_chart_version
#   release_github_create_flags
#   release_github_edit_flags
#   release_docs_alias
#
# Running it prints that decision:
#   hack/release-tag.sh v1.2.3-rc.1
#
# A tag is accepted when it is vMAJOR.MINOR.PATCH with an optional
# pre-release suffix and an optional build suffix. That covers the historical
# release regex (one suffix) and a pre-release that also has build metadata
# (v1.2.3-rc.1+exp.sha), which the single-suffix pattern rejected.
# Pre-release is not "any hyphen". Build metadata, the suffix after +, may
# contain hyphens and is still stable. A hyphen before + is a pre-release.
#
# Release tags never move the image latest tag. Pushes to main do that in
# release.yml and do not call this script. Stable tags move the mike latest
# alias. Pre-releases do not, and their GitHub Release is created with
# --prerelease and --latest=false.

# ghcr.io image repository. release.yml reads this; it is not a tag.
RELEASE_IMAGE_REPOSITORY="ghcr.io/kanzifucius/xr-rollout-controller"

classify_release_tag() {
  local tag without_build

  RELEASE_KIND=
  RELEASE_TAG=
  CHART_VERSION=
  IMAGE_LATEST=
  GITHUB_PRERELEASE=
  GITHUB_LATEST=
  DOCS_ALIAS_LATEST=
  CHART_RETAG_LATEST=

  if [[ $# -ne 1 || -z "${1}" ]]; then
    echo "::error::usage: classify_release_tag <git-tag>" >&2
    return 1
  fi
  tag="${1}"

  # Literal + is written as [+]. In bash =~ (ERE), a bare + is a quantifier.
  if [[ ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$ ]]; then
    echo "::error::Tag ${tag} is not a vMAJOR.MINOR.PATCH semver tag" >&2
    return 1
  fi

  RELEASE_TAG="${tag}"
  # Leading v only. Pre-release identifiers and build metadata stay.
  CHART_VERSION="${tag#v}"
  # Drop build metadata before looking for a pre-release hyphen.
  without_build="${CHART_VERSION%%+*}"
  # A release tag is the image tag. :latest moves on main, not here.
  IMAGE_LATEST=false
  # Never copy this chart onto a stable version or a latest chart tag.
  CHART_RETAG_LATEST=false

  if [[ "${without_build}" == *-* ]]; then
    RELEASE_KIND=prerelease
    GITHUB_PRERELEASE=true
    # Do not take GitHub's Latest badge from the last stable release.
    GITHUB_LATEST=false
    DOCS_ALIAS_LATEST=false
  else
    RELEASE_KIND=stable
    GITHUB_PRERELEASE=false
    # Stable keeps GitHub's automatic Latest selection.
    GITHUB_LATEST=auto
    DOCS_ALIAS_LATEST=true
  fi
}

require_classified() {
  if [[ -z "${RELEASE_KIND:-}" || -z "${RELEASE_TAG:-}" ]]; then
    echo "::error::classify_release_tag was not called" >&2
    return 1
  fi
}

# One image tag, the git tag. Refuses to add :latest.
release_image_tag_list() {
  local image

  require_classified || return 1
  if [[ $# -ne 1 || -z "${1}" ]]; then
    echo "::error::usage: release_image_tag_list <image-repository>" >&2
    return 1
  fi
  image="${1}"
  if [[ "${image}" == *:* ]]; then
    echo "::error::image repository must not include a tag" >&2
    return 1
  fi
  if [[ "${IMAGE_LATEST}" != "false" ]]; then
    echo "::error::Refusing to also tag ${RELEASE_TAG} as latest" >&2
    return 1
  fi
  printf '%s:%s\n' "${image}" "${RELEASE_TAG}"
}

# Chart version is the git tag with the leading v removed.
# Refuses a latest version and any retag of a stable or latest chart.
release_chart_version() {
  local core

  require_classified || return 1
  if [[ "${CHART_RETAG_LATEST}" != "false" ]]; then
    echo "::error::Refusing to retag a stable or latest chart for ${RELEASE_TAG}" >&2
    return 1
  fi
  if [[ -z "${CHART_VERSION}" || "${CHART_VERSION}" == "latest" ]]; then
    echo "::error::Refusing chart version '${CHART_VERSION}'" >&2
    return 1
  fi
  if [[ "v${CHART_VERSION}" != "${RELEASE_TAG}" ]]; then
    echo "::error::Chart version ${CHART_VERSION} is not ${RELEASE_TAG} without the leading v" >&2
    return 1
  fi
  core="${CHART_VERSION%%+*}"
  if [[ "${RELEASE_KIND}" == "prerelease" && "${core}" != *-* ]]; then
    echo "::error::Pre-release chart version ${CHART_VERSION} lost its pre-release suffix" >&2
    return 1
  fi
  printf '%s\n' "${CHART_VERSION}"
}

# Extra flags for `gh release create`, after --generate-notes.
# Stable: none (current GitHub Latest behaviour).
# Pre-release: --prerelease and --latest=false.
release_github_create_flags() {
  require_classified || return 1
  GITHUB_CREATE_FLAGS=()
  if [[ "${GITHUB_PRERELEASE}" == "true" ]]; then
    GITHUB_CREATE_FLAGS+=(--prerelease)
  elif [[ "${GITHUB_PRERELEASE}" != "false" ]]; then
    echo "::error::GITHUB_PRERELEASE must be true or false" >&2
    return 1
  fi
  if [[ "${GITHUB_LATEST}" == "false" ]]; then
    GITHUB_CREATE_FLAGS+=(--latest=false)
  elif [[ "${GITHUB_LATEST}" != "auto" ]]; then
    echo "::error::GITHUB_LATEST must be auto or false" >&2
    return 1
  fi
}

# Extra flags for `gh release edit` when the Release already exists.
# Pre-release marks the existing Release. Stable edits nothing.
release_github_edit_flags() {
  require_classified || return 1
  GITHUB_EDIT_FLAGS=()
  if [[ "${GITHUB_PRERELEASE}" == "true" ]]; then
    GITHUB_EDIT_FLAGS+=(--prerelease)
  elif [[ "${GITHUB_PRERELEASE}" != "false" ]]; then
    echo "::error::GITHUB_PRERELEASE must be true or false" >&2
    return 1
  fi
}

# Sets DOCS_ALIAS to "latest" for a stable tag, or empty for a pre-release.
# Empty means mike publishes the version and leaves the latest alias alone.
release_docs_alias() {
  require_classified || return 1
  if [[ "${DOCS_ALIAS_LATEST}" == "true" ]]; then
    DOCS_ALIAS=latest
  elif [[ "${DOCS_ALIAS_LATEST}" == "false" ]]; then
    DOCS_ALIAS=
  else
    echo "::error::DOCS_ALIAS_LATEST must be true or false (got ${DOCS_ALIAS_LATEST})" >&2
    return 1
  fi
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  set -euo pipefail
  if [[ $# -ne 1 ]]; then
    echo "usage: hack/release-tag.sh <git-tag>" >&2
    exit 2
  fi
  classify_release_tag "${1}"
  release_github_create_flags
  release_github_edit_flags
  release_docs_alias
  printf 'RELEASE_TAG=%s\n' "${RELEASE_TAG}"
  printf 'RELEASE_KIND=%s\n' "${RELEASE_KIND}"
  printf 'CHART_VERSION=%s\n' "$(release_chart_version)"
  printf 'IMAGE_LATEST=%s\n' "${IMAGE_LATEST}"
  printf 'GITHUB_PRERELEASE=%s\n' "${GITHUB_PRERELEASE}"
  printf 'GITHUB_LATEST=%s\n' "${GITHUB_LATEST}"
  printf 'DOCS_ALIAS_LATEST=%s\n' "${DOCS_ALIAS_LATEST}"
  printf 'CHART_RETAG_LATEST=%s\n' "${CHART_RETAG_LATEST}"
  printf 'IMAGE_TAGS=%s\n' "$(release_image_tag_list "${RELEASE_IMAGE_REPOSITORY}")"
  printf 'GITHUB_CREATE_FLAGS=%s\n' "${GITHUB_CREATE_FLAGS[*]-}"
  printf 'GITHUB_EDIT_FLAGS=%s\n' "${GITHUB_EDIT_FLAGS[*]-}"
  printf 'DOCS_ALIAS=%s\n' "${DOCS_ALIAS-}"
fi
