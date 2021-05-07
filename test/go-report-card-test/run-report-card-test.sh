#!/bin/bash
set -euo pipefail

THRESHOLD=98
SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
EXIT_CODE=0

function fail() {
    echo "❌ Test failed to meet go-report-card threshold score of: $THRESHOLD"
    exit 1
}
trap fail ERR

docker build --build-arg=GOPROXY=direct -t go-report-card-cli $SCRIPTPATH
if [[ $(docker run -it -v $SCRIPTPATH/../../:/app go-report-card-cli /go/bin/goimports -l /app/) ]]; then
    echo "❌ goimports found a problem in go source files. See above for the files with problems."
    EXIT_CODE=2
else
    echo "✅ goimports found no formatting errors in go source files"
fi

if grep -r -i -e 'cancelled' --exclude-dir={build,"$(basename $SCRIPTPATH)"} $SCRIPTPATH/../../* ; then
    echo "❌ Found a misspelling of 'canceled'!"
    EXIT_CODE=3
fi

docker run -t -v $SCRIPTPATH/../../:/app go-report-card-cli /go/bin/goreportcard-cli -v -t $THRESHOLD

exit $EXIT_CODE