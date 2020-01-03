# AWS Node Termination Handler

AWS Node Termination Handler Helm chart for Kubernetes. For more information on this project see the project repo at https://github.com/aws/aws-node-termination-handler.

## Prerequisites

* Kubernetes >= 1.11

## Installing the Chart

Add the EKS repository to Helm:
```sh
helm repo add eks https://aws.github.io/eks-charts
```
Install AWS Node Termination Handler:
To install the chart with the release name aws-node-termination-handler and default configuration:

```sh
helm install --name aws-node-termination-handler \
  --namespace kube-system eks/aws-node-termination-handler
```

To install into an EKS cluster where the Node Termination Handler is already installed, you can run:

```sh
helm upgrade --install --recreate-pods --force \
  aws-node-termination-handler --namespace kube-system eks/aws-node-termination-handler
```

If you receive an error similar to `Error: release aws-node-termination-handler
failed: <resource> "aws-node-termination-handler" already exists`, simply rerun
the above command.

The [configuration](#configuration) section lists the parameters that can be configured during installation.

## Uninstalling the Chart

To uninstall/delete the `aws-node-termination-handler` deployment:

```sh
helm delete --purge aws-node-termination-handler
```

The command removes all the Kubernetes components associated with the chart and deletes the release.

## Configuration

The following tables lists the configurable parameters of the chart and their default values.

Parameter | Description | Default
--- | --- | ---
`image.repository` | image repository | `amazon/aws-node-termination-handler`
`image.tag` | image tag | `<VERSION>`
`image.pullPolicy` | image pull policy | `IfNotPresent`
`deleteLocalData` | Tells kubectl to continue even if there are pods using emptyDir (local data that will be deleted when the node is drained). | `false`
`gracePeriod` | (DEPRECATED: Renamed to podTerminationGracePeriod) The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used. | `30`
`podTerminationGracePeriod` | The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used. | `30`
`nodeTerminationGracePeriod` | Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled based on this value to optimize the amount of compute time, but still safely drain the node before an event. | `120`
`ignoreDaemonsSets` | Causes kubectl to skip daemon set managed pods | `true`
`instanceMetadataURL` | The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing. | `http://169.254.169.254:80`
`affinity` | node/pod affinities | None
`podSecurityContext` | Pod Security Context | `{}`
`podAnnotations` | annotations to add to each pod | `{}`
`priorityClassName` | Name of the priorityClass | `system-node-critical`
`resources` | Resources for the pods | `requests.cpu: 50m, requests.memory: 64Mi, limits.cpu: 100m, limits.memory: 128Mi`
`securityContext` | Container Security context | `privileged: true`
`nodeSelector` | Tells the daemon set where to place the node-termination-handler pods. For example: `lifecycle: "Ec2Spot"`, `on-demand: "false"`, `aws.amazon.com/purchaseType: "spot"`, etc. Value must be a valid yaml expression. | `{}`
`tolerations` | list of node taints to tolerate | `[]`
`rbac.create` | if `true`, create and use RBAC resources | `true`
`rbac.pspEnabled` | If `true`, create and use a restricted pod security policy | `false`
`serviceAccount.create` | If `true`, create a new service account | `true`
`serviceAccount.name` | Service account to be used | None
`serviceAccount.annotations` | Specifies the annotations for ServiceAccount       | `{}`
