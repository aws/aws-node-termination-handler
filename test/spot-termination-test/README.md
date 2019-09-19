# Spot Termination Test

## Description:

The spot termination integration test orchestrates a local Kubernetes cluster using Kind (https://kind.sigs.k8s.io/), launches a pod to emulate the on-instance EC2 Metadata service to simulate a spot interruption, and then checks that the node-termination-handler has marked the nodes with a NoSchedule taint by querying the Kubernetes API.

## How do I run it?

1. You'll need docker installed locally and on your PATH.

2. `./run-spot-termination-test.sh`
