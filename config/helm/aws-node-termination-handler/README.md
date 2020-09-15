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
`image.pullSecrets` | image pull secrets (for private docker registries) | `[]`
`deleteLocalData` | Tells kubectl to continue even if there are pods using emptyDir (local data that will be deleted when the node is drained). | `false`
`gracePeriod` | (DEPRECATED: Renamed to podTerminationGracePeriod) The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used. | `30`
`podTerminationGracePeriod` | The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used. | `30`
`nodeTerminationGracePeriod` | Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled based on this value to optimize the amount of compute time, but still safely drain the node before an event. | `120`
`ignoreDaemonSets` | Causes kubectl to skip daemon set managed pods | `true`
`instanceMetadataURL` | The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing. | `http://169.254.169.254:80`
`webhookURL` | Posts event data to URL upon instance interruption action | ``
`webhookURLSecretName` | Pass Webhook URL as a secret. Secret Key: `webhookurl`, Value: `<WEBHOOK_URL>` | None
`webhookProxy` | Uses the specified HTTP(S) proxy for sending webhooks | ``
`webhookHeaders` | Replaces the default webhook headers. | `{"Content-type":"application/json"}`
`webhookTemplate` | Replaces the default webhook message template. | `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Description: {{ .Description }} - State: {{ .State }} - Start Time: {{ .StartTime }}"}`
`webhookTemplateConfigMapName` | Pass Webhook template file as configmap | None
`webhookTemplateConfigMapKey` | Name of the template file stored in the configmap| None
`dryRun` | If true, only log if a node would be drained | `false`
`enableScheduledEventDraining` | [EXPERIMENTAL] If true, drain nodes before the maintenance window starts for an EC2 instance scheduled event | `false`
`enableSpotInterruptionDraining` | If false, do not drain nodes when the spot interruption termination notice is received | `true`
`metadataTries` | The number of times to try requesting metadata. If you would like 2 retries, set metadata-tries to 3. | `3`
`cordonOnly` | If true, nodes will be cordoned but not drained when an interruption event occurs. | `false`
`taintNode` | If true, nodes will be tainted when an interruption event occurs. Currently used taint keys are `aws-node-termination-handler/scheduled-maintenance` and `aws-node-termination-handler/spot-itn` | `false`
`jsonLogging` | If true, use JSON-formatted logs instead of human readable logs. | `false`
`affinity` | node/pod affinities | None
`linuxAffinity` | Linux node/pod affinities | None
`windowsAffinity` | Windows node/pod affinities | None
`podAnnotations` | annotations to add to each pod | `{}`
`linuxPodAnnotations` | Linux annotations to add to each pod | `{}`
`windowsPodAnnotations` | Windows annotations to add to each pod | `{}`
`podLabels` | labels to add to each pod | `{}`
`linuxPodLabels` | labels to add to each Linux pod | `{}`
`windowsPodLabels` | labels to add to each Windows pod | `{}`
`priorityClassName` | Name of the priorityClass | `system-node-critical`
`resources` | Resources for the pods | `requests.cpu: 50m, requests.memory: 64Mi, limits.cpu: 100m, limits.memory: 128Mi`
`dnsPolicy` | DaemonSet DNS policy | Linux: `ClusterFirstWithHostNet`, Windows: `ClusterFirst`
`nodeSelector` | Tells the all daemon sets where to place the node-termination-handler pods. For example: `lifecycle: "Ec2Spot"`, `on-demand: "false"`, `aws.amazon.com/purchaseType: "spot"`, etc. Value must be a valid yaml expression. | `{}`
`linuxNodeSelector` | Tells the Linux daemon set where to place the node-termination-handler pods. For example: `lifecycle: "Ec2Spot"`, `on-demand: "false"`, `aws.amazon.com/purchaseType: "spot"`, etc. Value must be a valid yaml expression. | `{}`
`windowsNodeSelector` | Tells the Windows daemon set where to place the node-termination-handler pods. For example: `lifecycle: "Ec2Spot"`, `on-demand: "false"`, `aws.amazon.com/purchaseType: "spot"`, etc. Value must be a valid yaml expression. | `{}`
`tolerations` | list of node taints to tolerate | `[ {"operator": "Exists"} ]`
`rbac.create` | if `true`, create and use RBAC resources | `true`
`rbac.pspEnabled` | If `true`, create and use a restricted pod security policy | `false`
`serviceAccount.create` | If `true`, create a new service account | `true`
`serviceAccount.name` | Service account to be used | None
`serviceAccount.annotations` | Specifies the annotations for ServiceAccount       | `{}`
`procUptimeFile` | (Used for Testing) Specify the uptime file | `/proc/uptime`
`securityContext.runAsUserID` | User ID to run the container | `1000`
`securityContext.runAsGroupID` | Group ID to run the container | `1000`
`nodeSelectorTermsOs` | Operating System Node Selector Key | >=1.14: `kubernetes.io/os`, <1.14: `beta.kubernetes.io/os`
`nodeSelectorTermsArch` | CPU Architecture Node Selector Key | >=1.14: `kubernetes.io/arch`, <1.14: `beta.kubernetes.io/arch`
`targetNodeOs` | Space separated list of node OS's to target, e.g. "linux", "windows", "linux windows".  Note: Windows support is experimental. | `"linux"`
`enablePrometheusServer` | If true, start an http server exposing `/metrics` endpoint for prometheus. | `false`
`prometheusServerPort` | Replaces the default HTTP port for exposing prometheus metrics. | `9092`
`podMonitor.create` | if `true`, create a PodMonitor | `false`
`podMonitor.interval` | Prometheus scrape interval | `30s`
`podMonitor.sampleLimit` | Number of scraped samples accepted | `5000`
`podMonitor.labels` | Additional PodMonitor metadata labels | `{}`
`updateStrategy` | Update strategy for the all DaemonSets (Linux and Windows) | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`
`linuxUpdateStrategy` | Update strategy for the Linux DaemonSet | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`
`windowsUpdateStrategy` | Update strategy for the Windows DaemonSet | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`

## Metrics endpoint consideration
If prometheus server is enabled and since NTH is a daemonset with `host_networking=true`, nothing else will be able to bind to `:9092` (or the port configured) in the root network namespace
since it's listening on all interfaces.
Therefore, it will need to have a firewall/security group configured on the nodes to block access to the `/metrics` endpoint.
