#!/bin/bash

set -ex

GOPATH=$(pwd)/gopath
export GOPATH
export PATH=$PATH:/$GOPATH/bin
mkdir -p "${GOPATH}/bin"

pushd gopath/src/github.com/cloud-gov/cg-buildpack-notify
  go mod vendor
  go test -v
popd
