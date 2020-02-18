<h1>AWS Node Termination Handler</h1>

<h4>A Kubernetes Daemonset to gracefully handle EC2 instance shutdown</h4>

<p>
  <a href="https://github.com/kubernetes/kubernetes/releases">
    <img src="https://img.shields.io/badge/Kubernetes-%3E%3D%201.11-brightgreen" alt="kubernetes">
  </a>
  <a href="https://golang.org/doc/go1.13">
    <img src="https://img.shields.io/github/go-mod/go-version/aws/aws-node-termination-handler?color=blueviolet" alt="go-version">
  </a>
  <a href="https://opensource.org/licenses/Apache-2.0">
    <img src="https://img.shields.io/badge/License-Apache%202.0-ff69b4.svg" alt="license">
  </a>
  <a href="https://goreportcard.com/report/github.com/aws/aws-node-termination-handler">
    <img src="https://goreportcard.com/badge/github.com/aws/aws-node-termination-handler" alt="go-report-card">
  </a>
  <a href="https://travis-ci.org/aws/aws-node-termination-handler">
    <img src="https://travis-ci.org/aws/aws-node-termination-handler.svg?branch=master" alt="build-status">
  </a>
  <a href="https://codecov.io/gh/aws/aws-node-termination-handler">
    <img src="https://img.shields.io/codecov/c/github/aws/aws-node-termination-handler" alt="build-status">
  </a>
  <a href="https://hub.docker.com/r/amazon/aws-node-termination-handler">
    <img src="https://img.shields.io/docker/pulls/amazon/aws-node-termination-handler" alt="docker-pulls">
  </a>
</p>

<div>
<hr>
</div>

## Project Summary

This project ensures that the Kubernetes control plane responds appropriately to events that can cause your EC2 instance to become unavailable, such as [EC2 maintenance events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html) and [EC2 Spot interruptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-interruptions.html).  If not handled, your application code may not have enough time to stop gracefully, take longer to recover full availability, or accidentally schedule work to nodes that are going down. This handler will run a small pod on each host to perform monitoring and react accordingly.  When we detect an instance is going down, we use the Kubernetes API to cordon the node to ensure no new work is scheduled there, then drain it, removing any existing work.

The termination handler watches the [instance metadata service](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) to determine when to make requests to the Kubernetes API to mark the node as non-schedulable.  If the maintenance event is a reboot, we also apply a custom label to the node so when it restarts we remove the cordon.

You can run the termination handler on any Kubernetes cluster running on AWS, including self-managed clusters and those created with Amazon [Elastic Kubernetes Service](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html).

## Major Features

- Monitors EC2 Metadata for Scheduled Maintenance Events
- Monitors EC2 Metadata for Spot Instance Termination Notifications
- Helm installation and event configuration support
- Webhook feature to send shutdown or restart notification messages
- Unit & Integration Tests

## Installation and Configuration

The termination handler installs into your cluster a [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/), [ClusterRole](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), [ClusterRoleBinding](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), and a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). All four of these Kubernetes constructs are required for the termination handler to run properly.

The easiest way to install the termination handler is via [helm](https://helm.sh/).  The chart for this project is hosted in the [eks-charts](https://github.com/aws/eks-charts) repository.

To get started you need to add the eks-charts repo to helm

```
helm repo add eks https://aws.github.io/eks-charts
```

Once that is complete you can install the termination handler. We've provided some sample setup options below.

Basic installation (no configuration):
```sh
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  eks/aws-node-termination-handler
```

Basic installation without Helm:
```sh
kubectl apply -f https://github.com/aws/aws-node-termination-handler/releases/download/v1.2.0/all-resources.yaml 
```
For a full list of releases and associated artificats see our [releases page](https://github.com/aws/aws-node-termination-handler/releases).

Enabling Features:
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSpotInterruptionDraining="true" \
  --set enableScheduledEventDraining="false" \
  eks/aws-node-termination-handler
```

Running Only On Specific Nodes:
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set nodeSelector.lifecycle=spot \
  eks/aws-node-termination-handler
```

Webhook Configuration:
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set webhookURL=https://hooks.slack.com/services/YOUR/SLACK/URL \
  eks/aws-node-termination-handler
```

For a full list of configuration options see our [Helm readme](https://github.com/aws/eks-charts/tree/master/stable/aws-node-termination-handler).

## Building
For build instructions please consult [BUILD.md](./BUILD.md).

## Communication
* If you've run into a bug or have a new feature request, please open an [issue](https://github.com/aws/aws-node-termination-handler/issues/new).
* You can also chat with us in the [Kubernetes Slack](https://kubernetes.slack.com) in the `#provider-aws` channel

##  Contributing
Contributions are welcome! Please read our [guidelines](https://github.com/aws/aws-node-termination-handler/blob/master/CONTRIBUTING.md) and our [Code of Conduct](https://github.com/aws/aws-node-termination-handler/blob/master/CODE_OF_CONDUCT.md)

## License

This project is licensed under the Apache-2.0 License.
