#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROTO_DIR="$ROOT_DIR/protos"
GOOGLEAPIS_DIR="$ROOT_DIR/googleapis"
OUT_DIR="$ROOT_DIR/internal/genproto"
PROTOC_BIN="${PROTOC_BIN:-protoc}"

command -v "$PROTOC_BIN" >/dev/null 2>&1 || {
  echo "error: protoc is required but not installed" >&2
  exit 1
}

export PATH="$(go env GOPATH)/bin:$PATH"

for tool in "google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2" "google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0"; do
  bin_name="${tool##*/}"
  bin_name="${bin_name%@*}"
  if ! command -v "$bin_name" >/dev/null 2>&1; then
    echo "installing $tool"
    GO111MODULE=on go install "$tool"
  fi
done

cd "$ROOT_DIR"
shopt -s nullglob
set +u
# exclude hidden folders under $PROTO_DIR
folders=($(find "$PROTO_DIR" -mindepth 1 -maxdepth 1 -type d ! -name '.*'))
set -u
shopt -u nullglob

if [ ${#folders[@]} -eq 0 ]; then
  echo "no proto folders found under $PROTO_DIR" >&2
  exit 1
fi

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

for dir in "${folders[@]}"; do
  rel_path="${dir#"$PROTO_DIR"/}"
  mkdir -p "$OUT_DIR/$rel_path"
  mapfile -t files < <(find "$dir" -name "*.proto")
  if [ ${#files[@]} -eq 0 ]; then
    echo "skip $rel_path (no proto files)"
    continue
  fi
  echo
  echo "generating stubs for $rel_path"
  $PROTOC_BIN \
    -I "$PROTO_DIR" \
    -I "$GOOGLEAPIS_DIR" \
    --go_out="$ROOT_DIR" --go_opt=module=grpc-mock \
    --go-grpc_out="$ROOT_DIR" --go-grpc_opt=module=grpc-mock \
    "${files[@]}"
  echo "done $rel_path"
done
