[![Go Report Card](https://goreportcard.com/badge/github.com/aws/aws-node-termination-handler)](https://goreportcard.com/report/github.com/aws/aws-node-termination-handler) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
# AWS Node Termination Handler

The **AWS Node Termination Handler** is an operational [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/) built to run on any Kubernetes cluster using AWS [EC2 Spot Instances](https://aws.amazon.com/ec2/spot/). When a user starts the termination handler, the handler watches the AWS [instance metadata service](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) for [spot instance interruptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-interruptions.html) within a customer's account. If a termination notice is received for an instance that’s running on the cluster, the termination handler begins a multi-step cordon and drain process for the node.

You can run this termination handler on any Kubernetes cluster on AWS yourself by following the getting started instructions on the project Readme. For more information on Kubernetes + AWS you can explore the [EKS documentation](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html).

## Getting Started
The termination handler consists of a [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/), [ClusterRole](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), [ClusterRoleBinding](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), and a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). All four of these Kubernetes constructs are required for the termination handler to run properly.

You can create and run all of these at once on your own Kubernetes cluster by running the following command:
```
kubectl apply -f https://github.com/aws/aws-node-termination-handler/node-termination-handler.yaml
```
## Development
If you would like to build and run the project locally you can follow these steps:

Clone the repo:
```
git clone https://github.com/aws/aws-node-termination-handler
```
Build the latest version of the docker image:
```
make docker-build
```

## Communication
* Found a bug? Please open an issue.
* Have a feature request? Please open an issue.
* Want to contribute? Please submit a pull request.

##  Contributing
Contributions are welcome! Please read our [guidelines](https://github.com/aws/aws-node-termination-handler/blob/master/CONTRIBUTING.md) and our [Code of Conduct](https://github.com/aws/aws-node-termination-handler/blob/master/CODE_OF_CONDUCT.md)

## License
This project is licensed under the Apache-2.0 License.
