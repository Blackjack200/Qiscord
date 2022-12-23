#!/bin/bash

function build() {
  echo "[+] Build GOOS=${1} ARCH=${2}"
  CGO_ENABLED=0 GOOS=${1} GOARCH=${2} go build -o "qq_${1}_${2}" app.go
  mv "qq_${1}_${2}" bin/"qq_${1}_${2}"
}

rm -rdf bin 2>/dev/null >/dev/null
mkdir bin 2>/dev/null >/dev/null

build linux amd64 &
build darwin amd64 &
build windows amd64 &
wait