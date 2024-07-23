#!/bin/bash

cd "$(dirname "$0")"

# Needed Env Vars
# CF_API
# CF_API_SSL_FLAG (optional)
# CF_USER
# CF_PASS
# TEST_PASS

BUILDPACK_NAME="notify-test-buildpack"
BUILDPACK_VERSION_1_ZIP="https://github.com/cloudfoundry/binary-buildpack/releases/download/v1.0.13/binary-buildpack-v1.0.13.zip"
BUILDPACK_VERSION_2_ZIP="https://github.com/cloudfoundry/binary-buildpack/releases/download/v1.0.14/binary-buildpack-v1.0.14.zip"


ORG="test-notify-org"
SPACE="test-notify-space"
TEST_USER="notify-test-user@example.com"

RET=0

# Login
cf api $CF_API $CF_API_SSL_FLAG
cf auth $CF_USER "$CF_PASS"

# (Re)create org and space and make sure this user has permissions
cf create-org "$ORG"
cf create-space "$SPACE" -o "$ORG"
cf target -o "$ORG" -s "$SPACE"

cf create-user $TEST_USER "$TEST_PASS"
cf set-space-role $TEST_USER "$ORG" "$SPACE" SpaceManager
cf set-space-role $TEST_USER "$ORG" "$SPACE" SpaceDeveloper

# Delete any buildpacks beforehand
cf delete-buildpack $BUILDPACK_NAME -f

# Create buildpack
cf create-buildpack $BUILDPACK_NAME $BUILDPACK_VERSION_1_ZIP 100 --enable

pushd app || exit

  # build the app
  make

  # deploy the app
  make deploy

  # Run buildpack notify app
  pushd ../../
    go build && ./cg-buildpack-notify -notify > log.txt
    ## show the log.
    echo "Showing run log.."
    cat log.txt
    ## check log
    echo "Searching for text.."
    grep "Sent e-mail to" log.txt
    RESULT=$?
    if [ $RESULT -eq 0 ]; then
      echo "base case. should not find any users."
      RET=1
    else
      echo "base case success. shouldn't find any users."
    fi
  popd || exit

  # update the buildpack
  cf update-buildpack $BUILDPACK_NAME -p $BUILDPACK_VERSION_2_ZIP -i 100 --enable

  # Run buildpack notify app
  pushd ../../
    go build && ./cg-buildpack-notify -notify > log.txt
    ## show the log.
    echo "Showing run log.."
    cat log.txt
    ## check log
    echo "Searching for text.."
    grep "Sent e-mail to $TEST_USER" log.txt
    RESULT=$?
    if [ $RESULT -eq 0 ]; then
      echo "success"
    else
      echo "didn't send e-mail to user $TEST_USER"
      RET=1
    fi
  popd || exit

  # deploy the app again
  cf restage dummy-app

  # Run buildpack notify app
  pushd ../../
    go build && ./cg-buildpack-notify -notify > log.txt
    ## show the log.
    echo "Showing run log.."
    cat log.txt
    ## check log
    echo "Searching for text.."
    grep "Sent e-mail to" log.txt
    RESULT=$?
    if [ $RESULT -eq 0 ]; then
      echo "after update case. should not find any users"
      RET=1
    else
      echo "after update case success. shouldn't find any users."
    fi
  popd || exit
popd || exit

# clean up the app
cf delete dummy-app -f
# Delete the buildpack
cf delete-buildpack $BUILDPACK_NAME -f

exit $RET
