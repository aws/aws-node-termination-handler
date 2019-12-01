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
OVERRIDE_PATH=0

TMP_DIR=$SCRIPTPATH/tmp-$TEST_ID

KUSTOMIZATION_FILE="$TMP_DIR/kustomization.yaml"
NTH_OVERLAY_FILE="nth-image-overlay.yaml"
METADATA_OVERLAY_FILE="ec2-metadata-image-overlay.yaml"
REGULAR_POD_OVERLAY_FILE="ec2-metadata-regular-pod-image-overlay.yaml"

K8_1_16="kindest/node:v1.16.3@sha256:bced4bc71380b59873ea3917afe9fb35b00e174d22f50c7cab9188eac2b0fb88"
K8_1_15="kindest/node:v1.15.6@sha256:1c8ceac6e6b48ea74cecae732e6ef108bc7864d8eca8d211d6efb58d6566c40a"
K8_1_14="kindest/node:v1.14.9@sha256:00fb7d424076ed07c157eedaa3dd36bc478384c6d7635c5755746f151359320f"
K8_1_13="kindest/node:v1.13.12@sha256:ad1dd06aca2b85601f882ba1df4fdc03d5a57b304652d0e81476580310ba6289"
K8_1_12="kindest/node:v1.12.10@sha256:e93e70143f22856bd652f03da880bfc70902b736750f0a68e5e66d70de236e40"
K8_1_11="kindest/node:v1.11.10@sha256:44e1023d3a42281c69c255958e09264b5ac787c20a7b95caf2d23f8d8f3746f2"

K8_VERSION="$K8_1_16"
KIND_VERSION="0.6.0"

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
while getopts "pdv:i:n:e:o" opt; do
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
    o ) # Override path with your own kubectl and kind binaries
	OVERRIDE_PATH=1
      ;;
    \? )
        echo "$USAGE" 1>&2
        exit
      ;;
  esac
done

CLUSTER_NAME="$CLUSTER_NAME_BASE"-"$TEST_ID"
echo "🐳 Using Kubernetes $K8_VERSION"
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

## Append to the end of PATH so that the user can override the executables if they want
if [[ OVERRIDE_PATH -eq 1 ]]; then
   export PATH=$PATH:$TMP_DIR 
else
  if [ ! -x "$TMP_DIR/kubectl" ]; then
      echo "🥑 Downloading the \"kubectl\" binary"
      curl -Lo $TMP_DIR/kubectl "https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/$PLATFORM/amd64/kubectl"
      chmod +x $TMP_DIR/kubectl
      echo "👍 Downloaded the \"kubectl\" binary"
  fi

  if [ ! -x "$TMP_DIR/kind" ]; then
      echo "🥑 Downloading the \"kind\" binary"
      curl -Lo $TMP_DIR/kind https://github.com/kubernetes-sigs/kind/releases/download/v$KIND_VERSION/kind-$PLATFORM-amd64
      chmod +x $TMP_DIR/kind
      echo "👍 Downloaded the \"kind\" binary"
  fi
  export PATH=$TMP_DIR:$PATH
fi



echo "🥑 Creating k8s cluster using \"kind\""
kind delete cluster --name "$CLUSTER_NAME" || true
kind create cluster --name "$CLUSTER_NAME" --image $K8_VERSION --config "$SCRIPTPATH/kind-two-node-cluster.yaml" --wait "$CLUSTER_CREATION_TIMEOUT_IN_SEC"s --kubeconfig $TMP_DIR/kubeconfig
export KUBECONFIG="$TMP_DIR/kubeconfig"
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
    docker build $DOCKER_ARGS -t $DEFAULT_EC2_METADATA_DOCKER_IMG --no-cache --force-rm "$SCRIPTPATH/../../ec2-meta-data-proxy/."
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

POD_ID=$(kubectl get pods --namespace kube-system --kubeconfig $TMP_DIR/kubeconfig | grep -i node-termination-handler | cut -d' ' -f1)
kubectl logs $POD_ID --namespace kube-system -c node-termination-handler --kubeconfig $TMP_DIR/kubeconfig

echo "❌ Timed out after $(expr $TAINT_CHECK_CYCLES \* $TAINT_CHECK_SLEEP)sec checking for assertions..."
exit_and_fail
