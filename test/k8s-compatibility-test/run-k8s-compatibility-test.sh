#!/bin/bash
set -euo pipefail 

SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
versions=("1.29" "1.30" "1.31" "1.32")
E_CODE=0
AFTER_FIRST_RUN_ARGS=""
PASS_THRU_ARGS=""

USAGE=$(cat << 'EOM'
  Usage: run-k8s-compatability-test [-h]
  Executes the spot termination integration test for each version of kubernetes (k8s 1.29 - 1.32 supported)

  Examples:
          # run test with direct download of go modules
          run-k8s-compatability-test -p "-d"

          Optional:
            -p          Pass thru arguments to run-spot-termination-test.sh
            -h          Display help
EOM
)

# Process our input arguments
while getopts "p:" opt; do
  case ${opt} in
    p ) # PASS THRU ARGS
        PASS_THRU_ARGS="$OPTARG"
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

for i in "${!versions[@]}"; do
   version=${versions[$i]}
   $SCRIPTPATH/../k8s-local-cluster-test/run-test -b "test-${version//./-}" -v $version $PASS_THRU_ARGS $AFTER_FIRST_RUN_ARGS
   if [ $? -eq 0 ]; then
      echo "✅ Passed test for K8s version $version"
   else
      echo "❌ Failed test for K8s version $version"
      E_CODE=1
   fi
   AFTER_FIRST_RUN_ARGS="-n node-termination-handler:customtest -w webhook-test-proxy:customtest"
done

exit $E_CODE
