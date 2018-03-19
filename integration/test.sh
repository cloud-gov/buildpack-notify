#!/bin/bash

cd "$(dirname "$0")"

# Needed Env Vars
# CF_API
# CF_API_SSL_FLAG (optional)
# CF_USER
# CF_PASS
# TEST_PASS

BUILDPACK_NAME="notify-test-buildpack"

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

pushd apps
# test cases for whether we specify the buildpack or it is auto-detected.
for D in */; do
# if we want a specific directory for testing, comment out the above line
# and uncomment out the next two lines and adjust accordingly
# dirs=(python/)
# for D in $dirs; do
  source $D/.env
  for buildpack_option in "${buildpack_arg_options[@]}"; do
    echo "Running app $D with buildpack arg $buildpack_option"
    # Delete any buildpacks beforehand
    cf delete-buildpack $BUILDPACK_NAME -f

    # Create buildpack
    cf create-buildpack $BUILDPACK_NAME $BUILDPACK_VERSION_1_ZIP 1 --enable

    # For debugging purposes
    # CF_TRACE=1 cf buildpacks

    # deploy the app
    make deploy app="$D" buildpack="$buildpack_option"

    RESULT=$?
    if [ $RESULT -eq 0 ]; then
      echo "SUCCESS: app deployed successfully"
    else
      echo "FAIL: app did not deploy"
      RET=1
    fi

    # For debugging purposes
    # CF_TRACE=1 cf app dummy-app

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
      echo "FAIL: base case. should not find any users."
      RET=1
    else
      echo "SUCCESS: base case success. shouldn't find any users."
    fi
    popd

    # update the buildpack
    cf update-buildpack $BUILDPACK_NAME -p $BUILDPACK_VERSION_2_ZIP -i 1 --enable

    # For debugging purposes
    # CF_TRACE=1 cf buildpacks

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
      echo "SUCCESS: E-mail sent to $TEST_USER"
    else
      echo "FAIL: didn't send e-mail to user $TEST_USER"
      RET=1
    fi
    popd

    # deploy the app again
    cf restage dummy-app
    RESULT=$?
    if [ $RESULT -eq 0 ]; then
      echo "SUCCESS: app restaged successfully"
    else
      echo "FAIL: app did not restage"
      RET=1
    fi

    # For debugging purposes
    # CF_TRACE=1 cf app dummy-app

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
      echo "FAIL: after update case. should not find any users"
      RET=1
    else
      echo "SUCCESS: after update case success. shouldn't find any users."
    fi
    popd


    # clean up the app
    cf delete dummy-app -f
    # Delete the buildpack
    cf delete-buildpack $BUILDPACK_NAME -f
  done
done

popd

cf delete-user $TEST_USER -f

exit $RET
