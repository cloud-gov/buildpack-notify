#!/bin/bash

set -e -x

export GOPATH=$(pwd)/gopath
export PATH=$PATH:$GOPATH/bin

export IN_STATE="$(pwd)/${IN_STATE}"
export OUT_STATE="$(pwd)/${OUT_STATE}"

go get -u github.com/golang/dep/cmd/dep

cd gopath/src/github.com/18F/cg-buildpack-notify
dep ensure
go build

./cg-buildpack-notify
