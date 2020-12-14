#!/bin/bash
set -euo pipefail

SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
BUILD_BIN="$SCRIPTPATH/../../build/bin"

BINARY_NAME="node-termination-handler-linux-amd64"
LICENSE_TEST_TAG="nth-license-test"

SUPPORTED_PLATFORMS_LINUX="linux/amd64" make -s -f $SCRIPTPATH/../../Makefile build-binaries
docker build --build-arg=GOPROXY=direct -t $LICENSE_TEST_TAG $SCRIPTPATH/
docker run -i -e GITHUB_TOKEN --rm -v $SCRIPTPATH/:/test -v $BUILD_BIN/:/nth-bin $LICENSE_TEST_TAG golicense /test/license-config.hcl /nth-bin/$BINARY_NAME
