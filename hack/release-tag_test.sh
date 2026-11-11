#!/usr/bin/env bash
# Checks the stable versus pre-release split in hack/release-tag.sh and
# that the release and docs workflows consume that split.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=hack/release-tag.sh
source "${root}/hack/release-tag.sh"

failures=0

fail() {
  echo "FAIL $*" >&2
  failures=$((failures + 1))
}

check() {
  local label="$1" got="$2" want="$3"
  if [[ "${got}" != "${want}" ]]; then
    fail "${label}: got [${got}] want [${want}]"
  fi
}

joined_create_flags() {
  release_github_create_flags
  if ((${#GITHUB_CREATE_FLAGS[@]} > 0)); then
    printf '%s\n' "${GITHUB_CREATE_FLAGS[@]}"
  fi
}

joined_edit_flags() {
  release_github_edit_flags
  if ((${#GITHUB_EDIT_FLAGS[@]} > 0)); then
    printf '%s\n' "${GITHUB_EDIT_FLAGS[@]}"
  fi
}

# tag kind chart create-flags (newline separated) edit-flags docs-alias
expect() {
  local tag="$1" kind="$2" chart="$3" create_flags="$4" edit_flags="$5" docs_alias="$6"
  local got_chart got_image got_create got_edit

  if ! classify_release_tag "${tag}"; then
    fail "${tag}: rejected"
    return
  fi
  check "${tag} RELEASE_KIND" "${RELEASE_KIND}" "${kind}"
  check "${tag} IMAGE_LATEST" "${IMAGE_LATEST}" "false"
  check "${tag} CHART_RETAG_LATEST" "${CHART_RETAG_LATEST}" "false"

  if ! got_chart="$(release_chart_version)"; then
    fail "${tag}: release_chart_version failed"
    return
  fi
  check "${tag} chart version" "${got_chart}" "${chart}"

  if ! got_image="$(release_image_tag_list "${RELEASE_IMAGE_REPOSITORY}")"; then
    fail "${tag}: release_image_tag_list failed"
    return
  fi
  check "${tag} image tags" "${got_image}" "${RELEASE_IMAGE_REPOSITORY}:${tag}"

  if ! got_create="$(joined_create_flags)"; then
    fail "${tag}: release_github_create_flags failed"
    return
  fi
  check "${tag} gh create flags" "${got_create}" "${create_flags}"

  if ! got_edit="$(joined_edit_flags)"; then
    fail "${tag}: release_github_edit_flags failed"
    return
  fi
  check "${tag} gh edit flags" "${got_edit}" "${edit_flags}"

  if ! release_docs_alias; then
    fail "${tag}: release_docs_alias failed"
    return
  fi
  check "${tag} docs alias" "${DOCS_ALIAS}" "${docs_alias}"
}

expect_reject() {
  local tag="$1"
  if classify_release_tag "${tag}" 2>/dev/null; then
    fail "${tag}: accepted"
  fi
}

prerelease_create=$'--prerelease\n--latest=false'
prerelease_edit="--prerelease"

expect "v1.2.3" stable "1.2.3" "" "" "latest"
expect "v0.0.1" stable "0.0.1" "" "" "latest"
expect "v10.20.30" stable "10.20.30" "" "" "latest"
# Historical regex allows leading zeros. They stay stable.
expect "v01.2.3" stable "01.2.3" "" "" "latest"
# Build metadata is stable, including a hyphen after +.
expect "v1.2.3+sha" stable "1.2.3+sha" "" "" "latest"
expect "v1.2.3+linux-amd64" stable "1.2.3+linux-amd64" "" "" "latest"
expect "v1.2.3-rc.1" prerelease "1.2.3-rc.1" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-alpha.1" prerelease "1.2.3-alpha.1" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-rc" prerelease "1.2.3-rc" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-0" prerelease "1.2.3-0" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-RC.1" prerelease "1.2.3-RC.1" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-rc.1+exp.sha" prerelease "1.2.3-rc.1+exp.sha" "${prerelease_create}" "${prerelease_edit}" ""
expect "v1.2.3-alpha.1+linux-amd64" prerelease "1.2.3-alpha.1+linux-amd64" "${prerelease_create}" "${prerelease_edit}" ""

expect_reject "v1.2"
expect_reject "v1"
expect_reject "v1.2.3-"
expect_reject "v1.2.3+"
expect_reject "1.2.3"
expect_reject "V1.2.3"
expect_reject "latest"
expect_reject "v1.2.3-rc.1 "
expect_reject ""
if classify_release_tag 2>/dev/null; then
  fail "missing argument was accepted"
fi

# A failed classify must not leave a previous tag in place.
if release_chart_version 2>/dev/null; then
  fail "release_chart_version succeeded without a classified tag"
fi
if release_image_tag_list "${RELEASE_IMAGE_REPOSITORY}" 2>/dev/null; then
  fail "release_image_tag_list succeeded without a classified tag"
fi

classify_release_tag "v1.2.3-rc.1"
IMAGE_LATEST=true
if release_image_tag_list "${RELEASE_IMAGE_REPOSITORY}" 2>/dev/null; then
  fail "image latest tripwire did not fire"
fi
IMAGE_LATEST=false
CHART_RETAG_LATEST=true
if release_chart_version >/dev/null 2>&1; then
  fail "chart retag tripwire did not fire"
fi
CHART_RETAG_LATEST=false
GITHUB_LATEST=stable-latest
if release_github_create_flags 2>/dev/null; then
  fail "GITHUB_LATEST tripwire did not fire"
fi

cli_prerelease="$(bash "${root}/hack/release-tag.sh" "v1.2.3-rc.1")"
check "cli prerelease" "${cli_prerelease}" "$(cat <<EOF
RELEASE_TAG=v1.2.3-rc.1
RELEASE_KIND=prerelease
CHART_VERSION=1.2.3-rc.1
IMAGE_LATEST=false
GITHUB_PRERELEASE=true
GITHUB_LATEST=false
DOCS_ALIAS_LATEST=false
CHART_RETAG_LATEST=false
IMAGE_TAGS=${RELEASE_IMAGE_REPOSITORY}:v1.2.3-rc.1
GITHUB_CREATE_FLAGS=--prerelease --latest=false
GITHUB_EDIT_FLAGS=--prerelease
DOCS_ALIAS=
EOF
)"

cli_stable="$(bash "${root}/hack/release-tag.sh" "v1.2.3+linux-amd64")"
check "cli stable build metadata" "${cli_stable}" "$(cat <<EOF
RELEASE_TAG=v1.2.3+linux-amd64
RELEASE_KIND=stable
CHART_VERSION=1.2.3+linux-amd64
IMAGE_LATEST=false
GITHUB_PRERELEASE=false
GITHUB_LATEST=auto
DOCS_ALIAS_LATEST=true
CHART_RETAG_LATEST=false
IMAGE_TAGS=${RELEASE_IMAGE_REPOSITORY}:v1.2.3+linux-amd64
GITHUB_CREATE_FLAGS=
GITHUB_EDIT_FLAGS=
DOCS_ALIAS=latest
EOF
)"

if bash "${root}/hack/release-tag.sh" "v1.2" >/dev/null 2>&1; then
  fail "cli accepted v1.2"
fi
if bash "${root}/hack/release-tag.sh" >/dev/null 2>&1; then
  fail "cli accepted a missing argument"
fi

cd "${root}"

require_text() {
  local file="$1" text="$2"
  if ! grep -F -q -- "${text}" "${file}"; then
    fail "${file} missing text: ${text}"
  fi
}

require_text ".github/workflows/release.yml" "hack/release-tag.sh"
require_text ".github/workflows/release.yml" "classify_release_tag"
require_text ".github/workflows/release.yml" "release_image_tag_list"
require_text ".github/workflows/release.yml" "release_chart_version"
require_text ".github/workflows/release.yml" "release_github_create_flags"
require_text ".github/workflows/release.yml" "release_github_edit_flags"
require_text ".github/workflows/release.yml" "RELEASE_IMAGE_REPOSITORY"
require_text ".github/workflows/release.yml" "--generate-notes"
require_text ".github/workflows/release.yml" "dist/install.yaml"
require_text ".github/workflows/release.yml" "dist/compositionrollouts.crd.yaml"
require_text ".github/workflows/docs.yml" "hack/release-tag.sh"
require_text ".github/workflows/docs.yml" "classify_release_tag"
require_text ".github/workflows/docs.yml" "release_docs_alias"
require_text ".github/workflows/docs.yml" "release_chart_version"
require_text ".github/workflows/docs.yml" 'mike deploy --push --update-aliases "${VERSION}" "${DOCS_ALIAS}"'
require_text ".github/workflows/docs.yml" 'mike set-default --push "${DOCS_ALIAS}"'
require_text ".github/workflows/docs.yml" 'mike deploy --push "${VERSION}"'

if grep -F -n 'set-default --push latest' .github/workflows/docs.yml; then
  fail "docs workflow hardcodes the latest alias"
fi
if grep -F -n '== *-*' .github/workflows/docs.yml .github/workflows/release.yml; then
  fail "workflow still treats any hyphen as a pre-release"
fi
if grep -n 'v\[0-9\]' .github/workflows/release.yml .github/workflows/docs.yml; then
  fail "semver regex must live only in hack/release-tag.sh"
fi

latest_lines="$(grep -c ':latest' .github/workflows/release.yml || true)"
check "release.yml :latest lines" "${latest_lines}" "1"
helm_push_lines="$(grep -c 'helm push ' .github/workflows/release.yml || true)"
check "release.yml helm push lines" "${helm_push_lines}" "1"

if [[ "${failures}" -ne 0 ]]; then
  echo "${failures} assertion(s) failed" >&2
  exit 1
fi
echo "release tag classification ok"
