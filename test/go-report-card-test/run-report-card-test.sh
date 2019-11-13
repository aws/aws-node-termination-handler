#!/bin/bash
set -euo pipefail

THRESHOLD=90
SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"

function fail() {
    echo "❌ Test failed to meet go-report-card threshold score of: $THRESHOLD"
    exit 1
}
trap fail ERR

docker build --build-arg=GOPROXY=direct -t go-report-card-cli $SCRIPTPATH
docker run -it -v $SCRIPTPATH/../../:/app go-report-card-cli /go/bin/goreportcard-cli -v -t $THRESHOLD
