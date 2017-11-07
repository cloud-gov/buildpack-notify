#!/bin/bash

set -e -x

cf login -a $CF_API -u $CF_USERNAME -p $CF_PASSWORD -o $CF_ORGANIZATION -s $CF_SPACE

# Currently the output looks like this from the `run-task` cmd.
# cf run-task cg-buildpack-notify "bin/cg-buildpack-notify -notify -dry-run"
# Creating task for app cg-buildpack-notify in org my-org / space my-space as username...
# OK
#
# Task has been submitted successfully for execution.
# task name:   deadbeef
# task id:     1234
output=$(cf run-task cg-buildpack-notify "bin/cg-buildpack-notify -notify $ADDITIONAL_ARGS")

# Let's get the task id and task name.
task_id=$(echo "${output}" | grep -i "task id" | awk '{print $NF}')
task_name=$(echo "${output}" | grep -i "task name" | awk '{print $NF}')

# Keep waiting until the task is no longer in RUNNING state
# note: the first grep checks for task_id at the start of the line because the random generated task name SHA
# may also contain part of the id and it could be for another task.
elapsed=300
while [ "${elapsed}" -gt 0 ]; do
  if cf tasks cg-buildpack-notify | grep "^$task_id" | grep -qv "RUNNING"; then
    # if the task is no longer running, exit loop.
    break
  fi
  let elapsed-=5
  sleep 5
done
# note: if the loop ends, the script will end because of the `set -e` flag.

# print the logs
cf logs cg-buildpack-notify --recent | grep $task_name

if cf tasks cg-buildpack-notify | grep "^$task_id" | grep -q "FAILED"; then
  echo "task failed to complete."
  exit 1
fi

echo "task finished successfully"
