#!/bin/sh
# Prints this module's packages split by whether they reach liblogosdelivery.
#
#   scripts/test-packages.sh linked   packages that import internal/waku,
#                                     directly, transitively, or from a test
#   scripts/test-packages.sh pure     every other package
#
# For the Makefile's test targets. CGO_LDFLAGS is exported for the whole
# Makefile so that anything using the library finds it, but `go test -race`
# links runtime/cgo into every test binary, and a package with no cgo of its
# own then came out needing the library too — linked internally, without the
# rpath, so the loader could not find it. The pure set is built with the flags
# cleared; the linked set keeps them and is linked externally, rpath included.
#
# Computed rather than listed, because the list drifts: CI's hand-kept one had
# missed six packages by the time this replaced it.
set -eu

mode=${1:-}
case $mode in
  linked|pure) ;;
  *) echo "usage: $0 linked|pure" >&2; exit 2 ;;
esac

mod=$(go list -m)
waku="$mod/internal/waku"

# -test adds each package's test binary and test variants, so a package whose
# only use of the library is from a _test.go file is counted. ForTest names
# the package a variant belongs to; the test binary itself is "pkg.test".
linked=$(go list -e -test -f '{{if .ForTest}}{{.ForTest}}{{else}}{{.ImportPath}}{{end}}{{range .Deps}} {{.}}{{end}}' ./... \
  | awk -v w="$waku" '$1 == w { print $1; next } { for (i = 2; i <= NF; i++) if ($i == w) { print $1; break } }' \
  | sed 's/\.test$//' | sort -u)

case $mode in
  linked) printf '%s\n' "$linked" ;;
  pure)   go list ./... | grep -vxF "$linked" ;;
esac
