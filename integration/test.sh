#!/bin/bash

cd "$(dirname "$0")"

# Needed Env Vars
# CF_API
# CF_API_SSL_FLAG (optional)
# CF_USER
# CF_PASS

BUILDPACK_NAME="notify-test-buildpack"
BUILDPACK_VERSION_1_ZIP="https://github.com/cloudfoundry/binary-buildpack/releases/download/v1.0.13/binary-buildpack-v1.0.13.zip"
BUILDPACK_VERSION_2_ZIP="https://github.com/cloudfoundry/binary-buildpack/releases/download/v1.0.14/binary-buildpack-v1.0.14.zip"


ORG="test-notify-org"
SPACE="test-notify-space"

RET=0

# Login
cf api $CF_API $CF_API_SSL_FLAG 
cf auth $CF_USER "$CF_PASS"

# (Re)create org and space and make sure this user has permissions
cf create-org "$ORG"
cf create-space "$SPACE" -o "$ORG"
cf target -o "$ORG" -s "$SPACE"

cf set-space-role $CF_USER "$ORG" "$SPACE" SpaceDeveloper

# Delete any buildpacks beforehand
cf delete-buildpack $BUILDPACK_NAME -f

# Create buildpack
cf create-buildpack $BUILDPACK_NAME $BUILDPACK_VERSION_1_ZIP 100 --enable

pushd app
# deploy the app
make deploy

# Run buildpack notify app
pushd ../../
go build && ./cg-buildpack-notify > log.txt
## show the log.
cat log.txt
## check log
grep "Sent e-mail to" log.txt
RESULT=$?
if [ $RESULT -eq 0 ]; then
  echo "base case. should not find any users."
  RET=1
else
  echo "base case success. shouldn't find any users."
fi
popd

# update the buildpack
cf update-buildpack $BUILDPACK_NAME -p $BUILDPACK_VERSION_2_ZIP -i 100 --enable

# Run buildpack notify app
pushd ../../
go build && ./cg-buildpack-notify > log.txt
## show the log.
cat log.txt
## check log
grep "Sent e-mail to $CF_USER" log.txt
RESULT=$?
if [ $RESULT -eq 0 ]; then
  echo "success"
else
  echo "didn't send e-mail to user $CF_USER"
  RET=1
fi
popd

# deploy the app again
make deploy

# Run buildpack notify app
pushd ../../
go build && ./cg-buildpack-notify > log.txt
## show the log.
cat log.txt
## check log
grep "Sent e-mail to" log.txt
RESULT=$?
if [ $RESULT -eq 0 ]; then
  echo "after update case. should not find any users"
  RET=1
else
  echo "after update case success. shouldn't find any users."
fi
popd

popd

# clean up the app
cf delete dummy-app -f
# Delete the buildpack
cf delete-buildpack $BUILDPACK_NAME -f

exit $RET
