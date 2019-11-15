#!/bin/bash
set -euo pipefail
SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"

BINARY_NAME="handler"
BINARY_CONTAINER="extract"
BINARY_IMAGE_TAG="nth"
LICENSE_TEST_TAG="nth-license-test"

function clean_up {
    docker container rm $BINARY_CONTAINER || :
    docker image rm $BINARY_IMAGE_TAG || :
    rm -f $SCRIPTPATH/$BINARY_NAME || :
}

trap "clean_up" EXIT

docker build --build-arg=GOPROXY=direct -t $BINARY_IMAGE_TAG $SCRIPTPATH/../../
docker container create --name $BINARY_CONTAINER $BINARY_IMAGE_TAG
docker container cp $BINARY_CONTAINER:/$BINARY_NAME $SCRIPTPATH/$BINARY_NAME 
docker build --build-arg=GOPROXY=direct -t $LICENSE_TEST_TAG $SCRIPTPATH/
docker run -it -e GITHUB_TOKEN --rm -v $SCRIPTPATH/:/test $LICENSE_TEST_TAG golicense /test/license-config.hcl /test/$BINARY_NAME
