#!/bin/sh
set -eu

case "$(uname -s)" in
    Darwin|Linux) ;;
    *)
        printf '%s\n' "This installer supports macOS and Linux." >&2
        exit 1
        ;;
esac

for dependency in go arduino-cli; do
    if ! command -v "$dependency" >/dev/null 2>&1; then
        printf 'Missing dependency: %s\n' "$dependency" >&2
        printf '%s\n' "On macOS with Homebrew, run: brew install go arduino-cli" >&2
        exit 1
    fi
done

source_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
if [ ! -f "$source_dir/go.mod" ]; then
    printf '%s\n' "Run this installer from a Baobab checkout." >&2
    exit 1
fi

bin_dir=${BAOBAB_BIN_DIR:-"$HOME/.local/bin"}
mkdir -p "$bin_dir"
bin_dir=$(CDPATH= cd -P "$bin_dir" && pwd)
if [ -d "$bin_dir/bao" ]; then
    printf 'Cannot install over a directory: %s/bao\n' "$bin_dir" >&2
    exit 1
fi

stage_dir=$(mktemp -d "$source_dir/.bao-install.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf '%s\n' "Building Baobab..."
cd "$source_dir"
go build -o "$stage_dir/bao" .
"$stage_dir/bao" --version
mv -f "$stage_dir/bao" "$source_dir/bao"

if [ "$bin_dir" != "$source_dir" ]; then
    ln -sfn "$source_dir/bao" "$bin_dir/bao"
fi
printf 'Installed %s/bao\n' "$bin_dir"
printf '%s\n' "Run bao from your Arduino workspace."

case ":${PATH:-}:" in
    *:"$bin_dir":*) ;;
    *)
        printf '\n%s\n' "Add this line to your shell configuration, then open a new terminal:"
        printf 'export PATH="%s:$PATH"\n' "$bin_dir"
        ;;
esac
