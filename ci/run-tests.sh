#!/bin/sh

set -e -x

export GOPATH=$(pwd)/gopath

cd gopath/src/github.com/18F/cg-buildpack-notify
glide install
go test $(go list ./... | grep -v /vendor/)
