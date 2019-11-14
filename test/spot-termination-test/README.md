# Spot Termination Test

## Description:

The spot termination integration test orchestrates a local Kubernetes cluster using Kubernetes-in-Docker (https://kind.sigs.k8s.io/) and then simulates a spot interruption.

## Test Sequence

1. Downloads the kind and kubectl binary. Creates a local, 2-node kubernetes cluster w/ kind.

2. Launches a daemonset pod w/ the node-termination-handler and an ec2-meta-data-proxy container which simulates a spot interruption 

3. Launches a kubernetes deployment which creates a pod with the ec2-meta-data-proxy. Nothing is using the data from the proxy, it's just being used as a non-daemonset pod to test eviction of the node

4. Asserts that the kubernetes deployment scheduled and started the regular pod (ec2-meta-data-proxy)

5. ec2-meta-data-proxy will trigger an interruption within the daemonset pod

6. Asserts that the node is marked with a NoSchedule (cordoned)

7. Asserts that the regular pod (ec2-meta-data-proxy) was evicted from the node

## How do I run it?

1. You'll need docker installed locally and on your PATH.

2. `./run-spot-termination-test.sh`  

3. You can use `--help` for all the optional CLI arguments
