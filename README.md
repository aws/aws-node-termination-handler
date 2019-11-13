![](https://img.shields.io/badge/Kubernetes-%3E%3D%201.12-brightgreen) ![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/aws/aws-node-termination-handler?color=blueviolet) [![License](https://img.shields.io/badge/License-Apache%202.0-ff69b4.svg)](https://opensource.org/licenses/Apache-2.0) [![Go Report Card](https://goreportcard.com/badge/github.com/aws/aws-node-termination-handler)](https://goreportcard.com/report/github.com/aws/aws-node-termination-handler)  [![Build Status](https://travis-ci.org/aws/aws-node-termination-handler.svg?branch=master)](https://travis-ci.org/aws/aws-node-termination-handler) ![Docker Pulls](https://img.shields.io/docker/pulls/amazon/aws-node-termination-handler)
# AWS Node Termination Handler

The **AWS Node Termination Handler** is an operational [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/) built to run on any Kubernetes cluster using AWS [EC2 Spot Instances](https://aws.amazon.com/ec2/spot/). When a user starts the termination handler, the handler watches the AWS [instance metadata service](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) for [spot instance interruptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-interruptions.html) within a customer's account. If a termination notice is received for an instance that’s running on the cluster, the termination handler begins a multi-step cordon and drain process for the node.

You can run the termination handler on any Kubernetes cluster running on AWS, including clusters created with Amazon [Elastic Kubernetes Service](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html).

## Getting Started
The termination handler consists of a [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/), [ClusterRole](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), [ClusterRoleBinding](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), and a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). All four of these Kubernetes constructs are required for the termination handler to run properly.

You can create and run all of these at once on your own Kubernetes cluster by running the following command:
```
kubectl apply -k https://github.com/aws/aws-node-termination-handler/config/base?ref=master
```

By default the termination handler will run on all nodes in your cluster, but will only cordon and drain your Spot instances upon receiving a termination notification.

For build instructions please consult [BUILD.md](./BUILD.md).

## Communication
* Found a bug? Please open an issue.
* Have a feature request? Please open an issue.
* Want to contribute? Please submit a pull request.

##  Contributing
Contributions are welcome! Please read our [guidelines](https://github.com/aws/aws-node-termination-handler/blob/master/CONTRIBUTING.md) and our [Code of Conduct](https://github.com/aws/aws-node-termination-handler/blob/master/CODE_OF_CONDUCT.md)

## License
This project is licensed under the Apache-2.0 License.
