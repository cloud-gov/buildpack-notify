#!/bin/bash

set -e -x

export GOPATH=$(pwd)/gopath
export PATH=$PATH:$GOPATH/bin

cd gopath/src/github.com/18F/cg-buildpack-notify
go build

./cg-buildpack-notify
