#!/bin/sh
# Invoked by .goreleaser.yaml's `signs` block during `goreleaser release`. Not
# meant to be run directly.
#
# Exists only because `go run ./cmd/nrsign` has to run with the backend Go
# module as its working directory (the repo root isn't a module), but
# goreleaser's own ${artifact}-style substitution in signs.args/cmd is a
# generic $NAME/${NAME} expansion (Go's os.Expand): it doesn't just replace
# ${artifact}, it matches *any* $-prefixed token in the string, including a
# literal "$0" meant as a POSIX positional parameter for the nested shell.
# An inline `sh -c '... "$0"'` in the YAML gets its "$0" silently expanded to
# an empty string (unknown var name -> empty) before sh ever sees it. Keeping
# the positional-parameter reference inside a real script file instead of a
# YAML string sidesteps goreleaser's templating entirely.
set -e

artifact="$1"
case "$artifact" in
    /*) ;;                            # already absolute
    *)  artifact="$PWD/$artifact" ;;  # resolve relative to goreleaser's cwd (repo root)
esac

cd "$(dirname "$0")/../backend"
exec go run ./cmd/nrsign "$artifact"
