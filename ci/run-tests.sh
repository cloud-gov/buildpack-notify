#!/bin/sh

set -e -x

export GOPATH=$(pwd)/gopath
export PATH=$PATH:/$GOPATH/bin

go get -u github.com/golang/dep/cmd/dep

cd gopath/src/github.com/18F/cg-buildpack-notify
dep ensure
go test -v $(go list ./... | grep -v /vendor/)
