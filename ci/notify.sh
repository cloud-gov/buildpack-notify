#!/bin/bash

set -ex

GOPATH=$(pwd)/gopath
export GOPATH
export PATH=$PATH:$GOPATH/bin
mkdir -p "${GOPATH}/bin"

IN_STATE="$(pwd)/${IN_STATE}"
OUT_STATE="$(pwd)/${OUT_STATE}"

export IN_STATE
export OUT_STATE

pushd gopath/src/github.com/cloud-gov/buildpack-notify
  go mod vendor
  go build
  ./buildpack-notify
popd
