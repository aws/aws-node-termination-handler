#!/bin/bash
set -euo pipefail

START=$(date +%s)
SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
PLATFORM=$(uname | tr '[:upper:]' '[:lower:]')
CLUSTER_NAME_BASE="spot-termination-test"
CLUSTER_CREATION_TIMEOUT_IN_SEC=300
TEST_ID=$(uuidgen | cut -d'-' -f1 | tr '[:upper:]' '[:lower:]')
PRESERVE=false
TAINT_CHECK_CYCLES=15
TAINT_CHECK_SLEEP=15

DOCKER_ARGS=""
DEFAULT_NODE_TERMINATION_HANDLER_DOCKER_IMG="node-termination-handler:customtest"
NODE_TERMINATION_HANDLER_DOCKER_IMG=""
DEFAULT_EC2_METADATA_DOCKER_IMG="ec2-meta-data-proxy:customtest"
EC2_METADATA_DOCKER_IMG=""

TMP_DIR=$SCRIPTPATH/tmp-$TEST_ID

KUSTOMIZATION_FILE="$TMP_DIR/kustomization.yaml"
NTH_OVERLAY_FILE="nth-image-overlay.yaml"
METADATA_OVERLAY_FILE="ec2-metadata-image-overlay.yaml"
REGULAR_POD_OVERLAY_FILE="ec2-metadata-regular-pod-image-overlay.yaml"

K8_1_16="kindest/node:v1.16.2@sha256:2c68d327c2833fa9c0f81b5fd36499cf969646cd50affecd21b4725d37931e21"
K8_1_15="kindest/node:v1.15.3@sha256:27e388752544890482a86b90d8ac50fcfa63a2e8656a96ec5337b902ec8e5157"
K8_1_14="kindest/node:v1.14.6@sha256:464a43f5cf6ad442f100b0ca881a3acae37af069d5f96849c1d06ced2870888d"
K8_1_13="kindest/node:v1.13.10@sha256:2f5f882a6d0527a2284d29042f3a6a07402e1699d792d0d5a9b9a48ef155fa2a"
K8_1_12="kindest/node:v1.12.10@sha256:e43003c6714cc5a9ba7cf1137df3a3b52ada5c3f2c77f8c94a4d73c82b64f6f3"
K8_1_11="kindest/node:v1.11.10@sha256:bb22258625199ba5e47fb17a8a8a7601e536cd03456b42c1ee32672302b1f909"

K8_VERSION="$K8_1_15"

USAGE=$(cat << 'EOM'
  Usage: run-spot-termination-test [-p]
  Executes the spot termination integration test for the Node Termination Handler

  Example: run-spot-termination-test -p -i my-test-k8s-1.15 -v 1.15 -n node-termination-handler:customtest

          Optional:
            -p          Preserve kind k8s cluster for inspection
            -i          Test Identifier to suffix Cluster Name and tmp dir
            -v          K8s version to use in this test
            -n          Node Termination Handler Docker Image
            -e          EC2 Metadata Docker Image 
            -d          use GOPROXY=direct to bypass proxy.golang.org
EOM
)

# Process our input arguments
while getopts "pdv:i:n:e:" opt; do
  case ${opt} in
    p ) # PRESERVE K8s Cluster
        echo "❄️ This run will preserve the cluster as you requested"
        PRESERVE=true
      ;;
    i ) # Test name
        TEST_ID=$OPTARG
        echo "👉 Test Run: $TEST_ID 👈"
      ;;
    v ) # K8s version to test
        OPTARG="K8_`echo $OPTARG | sed 's/\./\_/g'`"
        if [ ! -z ${OPTARG+x} ]; then
            K8_VERSION=${!OPTARG}
        else 
            echo "K8s version not supported"
            exit 2
        fi
      ;;
    n ) # Node Termination Handler Docker Image 
        NODE_TERMINATION_HANDLER_DOCKER_IMG=$OPTARG
      ;;
    e ) # EC2 Metadata Docker Image
        EC2_METADATA_DOCKER_IMG=$OPTARG
      ;;
    d ) # use GOPROXY=direct
        DOCKER_ARGS="--build-arg GOPROXY=direct"
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

CLUSTER_NAME="$CLUSTER_NAME_BASE"-"$TEST_ID"
echo "🐳 Using Kubernetes $K8_VERSION"

## Append to the end of PATH so that the user can override the executables if they want
export PATH=$PATH:$TMP_DIR
mkdir -p $TMP_DIR

function clean_up {
    if [[ "$PRESERVE" == false ]]; then
        rm -rf $TMP_DIR
        kind delete cluster --name $CLUSTER_NAME || :
    fi 
}

function exit_and_fail {
    END=$(date +%s)
    echo "⏰ Took $(expr $END - $START)sec"
    echo "❌ Spot Termination Test FAILED $TEST_ID! ❌"
    exit 1
}
trap "exit_and_fail" INT TERM ERR
trap "clean_up" EXIT

deps=("docker")

for dep in "${deps[@]}"; do
    path_to_executable=$(which $dep)
    if [ ! -x "$path_to_executable" ]; then
        echo "You are required to have $dep installed on your system..."
        echo "Please install $dep and try again. "
        exit -1
    fi
done

if [ ! -x "$TMP_DIR/kubectl" ]; then
    echo "🥑 Downloading the \"kubectl\" binary"
    curl -Lo $TMP_DIR/kubectl "https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/$PLATFORM/amd64/kubectl"
    chmod +x $TMP_DIR/kubectl
    echo "👍 Downloaded the \"kubectl\" binary"
fi

if [ ! -x "$TMP_DIR/kind" ]; then
    echo "🥑 Downloading the \"kind\" binary"
    curl -Lo $TMP_DIR/kind https://github.com/kubernetes-sigs/kind/releases/download/v0.5.1/kind-$PLATFORM-amd64
    chmod +x $TMP_DIR/kind
    echo "👍 Downloaded the \"kind\" binary"
fi

echo "🥑 Creating k8s cluster using \"kind\""
kind delete cluster --name "$CLUSTER_NAME" || true
kind create cluster --name "$CLUSTER_NAME" --image $K8_VERSION --config "$SCRIPTPATH/kind-two-node-cluster.yaml" --wait "$CLUSTER_CREATION_TIMEOUT_IN_SEC"s
export KUBECONFIG="$(kind get kubeconfig-path --name=$CLUSTER_NAME)"
echo "👍 Created k8s cluster using \"kind\" and added kube config to KUBECONFIG"

cat <<-EOF > $KUSTOMIZATION_FILE
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
- ../../../config/overlays/test

patchesStrategicMerge:
EOF

if [ -z "$NODE_TERMINATION_HANDLER_DOCKER_IMG" ]; then 
    echo "🥑 Building the node-termination-handler docker image"
    docker build $DOCKER_ARGS -t $DEFAULT_NODE_TERMINATION_HANDLER_DOCKER_IMG --no-cache --force-rm "$SCRIPTPATH/../../." 
    NODE_TERMINATION_HANDLER_DOCKER_IMG="$DEFAULT_NODE_TERMINATION_HANDLER_DOCKER_IMG"
    echo "👍 Built the node-termination-handler docker image"
else 
    cat <<-EOF > $TMP_DIR/$NTH_OVERLAY_FILE
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-termination-handler
spec:
  template:
    spec:
      containers:
      - name: node-termination-handler
        image: $NODE_TERMINATION_HANDLER_DOCKER_IMG
EOF
    echo "- $NTH_OVERLAY_FILE" >> $KUSTOMIZATION_FILE
    echo "🥑 Skipping building the node-termination-handler docker image, since one was specified ($NODE_TERMINATION_HANDLER_DOCKER_IMG)"
fi

if [ -z "$EC2_METADATA_DOCKER_IMG" ]; then 
    echo "🥑 Building the ec2-meta-data-proxy docker image"
    docker build $DOCKER_ARGS -t $DEFAULT_EC2_METADATA_DOCKER_IMG --no-cache --force-rm "$SCRIPTPATH/."
    EC2_METADATA_DOCKER_IMG="$DEFAULT_EC2_METADATA_DOCKER_IMG"
    echo "👍 Built the ec2-meta-data-proxy docker image"
else 
    cat <<-EOF > $TMP_DIR/$METADATA_OVERLAY_FILE
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-termination-handler
spec:
  template:
    spec:
      containers:
      - name: ec2-metadata-proxy
        image: $EC2_METADATA_DOCKER_IMG
EOF
    echo "- $METADATA_OVERLAY_FILE" >> $KUSTOMIZATION_FILE
        cat <<-EOF > $TMP_DIR/$REGULAR_POD_OVERLAY_FILE
apiVersion: apps/v1
kind: Deployment
metadata:
  name: regular-pod-test
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: ec2-meta-data-proxy
        image: $EC2_METADATA_DOCKER_IMG
EOF
    echo "- $REGULAR_POD_OVERLAY_FILE" >> $KUSTOMIZATION_FILE
    echo "🥑 Skipping building the ec2-meta-data-proxy docker image, since one was specified ($EC2_METADATA_DOCKER_IMG)"
fi

echo "🥑 Tagging worker nodes to execute integ test"
kubectl label nodes $CLUSTER_NAME-worker lifecycle=Ec2Spot
kubectl label nodes $CLUSTER_NAME-worker app=spot-termination-test
echo "👍 Tagged worker nodes to execute integ test"

echo "🥑 Loading both images into the cluster"
kind load docker-image --name $CLUSTER_NAME --nodes=$CLUSTER_NAME-worker,$CLUSTER_NAME-control-plane $NODE_TERMINATION_HANDLER_DOCKER_IMG
kind load docker-image --name $CLUSTER_NAME --nodes=$CLUSTER_NAME-worker,$CLUSTER_NAME-control-plane $EC2_METADATA_DOCKER_IMG
echo "👍 Loaded both images into the cluster"

echo "🥑 Applying the test overlay kustomize config to k8s using kubectl"
kubectl apply -k "$TMP_DIR"

for i in `seq 1 $TAINT_CHECK_CYCLES`; do
    if kubectl get events | grep Started; then
        echo "✅ Verified regular-pod-test pod was scheduled and started!"
        if kubectl get nodes $CLUSTER_NAME-worker | grep SchedulingDisabled; then
            echo "✅ Verified the worker node was cordoned!"
            if [ -z "$(kubectl get pods --namespace=default -o jsonpath="{.items[].spec.nodeName}" | grep worker)" ]; then
                echo "✅ Verified the regular-pod-test pod was evicted!"
                END=$(date +%s)
                echo "⏰ Took $(expr $END - $START)sec"
                echo "✅ Spot Termination Test Passed $TEST_ID! ✅"
                exit 0
            fi
        fi
    fi
    sleep $TAINT_CHECK_SLEEP
done

echo "❌ Timed out after $(expr $TAINT_CHECK_CYCLES \* $TAINT_CHECK_SLEEP)sec checking for assertions..."
exit_and_fail
