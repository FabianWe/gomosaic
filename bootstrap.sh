#!/bin/bash

VERSION="1.1-beta"

LINUX="linux"
LINUX_ARCH=("amd64" "arm" "arm64" "386")

WINDOWS="windows"
WINDOWS_ARCH=("386" "amd64")

CHECKSUMS="gomosaic_${VERSION}_checksums.txt"
touch "compiled/$CHECKSUMS"

function createlinux {
  printf "Compiling for %s with architecture %s\n" "$1" "$2"
  local OUT_FILE="gomosaic_${VERSION}_${1}-${2}"
  GOOS="$1" GOARCH="$2" go build -o "compiled/$OUT_FILE" "cmd/mosaic/mosaic.go"
  tar -czf "compiled/$OUT_FILE.tar.gz" "LICENSE" "README.md" "AUTHORS" -C "compiled/" "$OUT_FILE"
  ( cd "compiled/" && sha256sum "$OUT_FILE.tar.gz" >> "$CHECKSUMS" )
}

function createwindows {
  printf "Compiling for %s with architecture %s\n" "$1" "$2"
  local OUT_FILE="gomosaic_${VERSION}_${1}-${2}"
  GOOS="$1" GOARCH="$2" go build -o "compiled/$OUT_FILE.exe" "cmd/mosaic/mosaic.go"
  zip "-j" "-q" "compiled/$OUT_FILE.zip" "LICENSE" "README.md" "AUTHORS" "compiled/$OUT_FILE.exe"
  ( cd "compiled/" && sha256sum "$OUT_FILE.zip" >> "$CHECKSUMS" )
}

for var in "${LINUX_ARCH[@]}"
do
  createlinux "$LINUX" "$var"
done

for var in "${WINDOWS_ARCH[@]}"
do
  createwindows "$WINDOWS" "$var"
done
