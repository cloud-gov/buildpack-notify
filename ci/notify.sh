#!/bin/sh

set -e -x

cf login -a $CF_API -u $CF_USERNAME -p $CF_PASSWORD -o $CF_ORGANIZATION -s $CF_SPACE

cf run-task cg-buildpack-notify "bin/cg-buildpack-notify -notify $ADDITIONAL_ARGS"
