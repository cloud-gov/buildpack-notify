#!/bin/bash

set -ex

export GOPATH=$(pwd)/gopath
export PATH=$PATH:/$GOPATH/bin
mkdir -p ${GOPATH}/bin

curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

pushd gopath/src/github.com/cloud-gov/cg-buildpack-notify
  dep ensure
  go test -v $(go list ./... | grep -v /vendor/)
popd
