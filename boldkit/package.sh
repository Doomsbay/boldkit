#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${root_dir}/.." && pwd)"
dist_dir="${DIST_DIR:-${root_dir}/dist}"
platforms_raw="${PLATFORMS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64}"
read -r -a platforms <<< "${platforms_raw}"

if [[ -n "${VERSION:-}" ]]; then
  version="${VERSION}"
else
  if version="$(git -C "${root_dir}" describe --tags --abbrev=0 2>/dev/null)"; then
    : # use git tag
  else
    version="dev-$(date +%Y%m%d)"
  fi
fi

mkdir -p "${dist_dir}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

echo "Packaging BoldKit version: ${version}"
echo "Platforms: ${platforms_raw}"
echo "Dist dir: ${dist_dir}"

for plat in "${platforms[@]}"; do
  GOOS="${plat%%/*}"
  GOARCH="${plat##*/}"
  ext=""
  archive_ext="tar.gz"
  if [[ "${GOOS}" == "windows" ]]; then
    ext=".exe"
  fi

  out_dir="${tmp_dir}/boldkit_${version}_${GOOS}_${GOARCH}"
  bin_name="binaries/boldkit${ext}"
  mkdir -p "${out_dir}/binaries"

  echo "Building ${GOOS}/${GOARCH}..."
  (cd "${repo_root}" && GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=0 go build -o "${out_dir}/${bin_name}" ./boldkit)

  cp "${repo_root}/LICENSE" "${out_dir}/"

  archive_name="boldkit_${version}_${GOOS}_${GOARCH}.${archive_ext}"
  (cd "${tmp_dir}" && tar -czf "${dist_dir}/${archive_name}" "$(basename "${out_dir}")")

done

echo "Done. Artifacts in ${dist_dir}"
