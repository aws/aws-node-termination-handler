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
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  eks/aws-node-termination-handler
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

### AWS Node Termination Handler Common Configuration

The configuration in this table applies to both queue-processor mode and IMDS mode.

Parameter | Description | Default
--- | --- | ---
`deleteLocalData` | Tells kubectl to continue even if there are pods using emptyDir (local data that will be deleted when the node is drained). | `true`
`gracePeriod` | (DEPRECATED: Renamed to podTerminationGracePeriod) The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used, which defaults to 30 seconds if not specified. | `-1`
`podTerminationGracePeriod` | The time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used, which defaults to 30 seconds if not specified. | `-1`
`nodeTerminationGracePeriod` | Period of time in seconds given to each NODE to terminate gracefully. Node draining will be scheduled based on this value to optimize the amount of compute time, but still safely drain the node before an event. | `120`
`ignoreDaemonSets` | Causes kubectl to skip daemon set managed pods | `true`
`instanceMetadataURL` | The URL of EC2 instance metadata. This shouldn't need to be changed unless you are testing. | `http://169.254.169.254:80`
`webhookURL` | Posts event data to URL upon instance interruption action | ``
`webhookURLSecretName` | Pass Webhook URL as a secret. Secret Key: `webhookurl`, Value: `<WEBHOOK_URL>` | None
`webhookProxy` | Uses the specified HTTP(S) proxy for sending webhooks | ``
`webhookHeaders` | Replaces the default webhook headers. | `{"Content-type":"application/json"}`
`webhookTemplate` | Replaces the default webhook message template. | `{"text":"[NTH][Instance Interruption] EventID: {{ .EventID }} - Kind: {{ .Kind }} - Instance: {{ .InstanceID }} - Node: {{ .NodeName }} - Description: {{ .Description }} - Start Time: {{ .StartTime }}"}`
`webhookTemplateConfigMapName` | Pass Webhook template file as configmap | None
`webhookTemplateConfigMapKey` | Name of the template file stored in the configmap| None
`metadataTries` | The number of times to try requesting metadata. If you would like 2 retries, set metadata-tries to 3. | `3`
`cordonOnly` | If true, nodes will be cordoned but not drained when an interruption event occurs. | `false`
`taintNode` | If true, nodes will be tainted when an interruption event occurs. Currently used taint keys are `aws-node-termination-handler/scheduled-maintenance`, `aws-node-termination-handler/spot-itn`, `aws-node-termination-handler/asg-lifecycle-termination` and `aws-node-termination-handler/rebalance-recommendation`| `false`
`jsonLogging` | If true, use JSON-formatted logs instead of human readable logs. | `false`
`logLevel` | Sets the log level (INFO, DEBUG, or ERROR) | `INFO`
`enablePrometheusServer` | If true, start an http server exposing `/metrics` endpoint for prometheus. | `false`
`prometheusServerPort` | Replaces the default HTTP port for exposing prometheus metrics. | `9092`
`enableProbesServer` | If true, start an http server exposing `/healthz` endpoint for probes. | `false`
`probesServerPort` | Replaces the default HTTP port for exposing probes endpoint. | `8080`
`probesServerEndpoint` | Replaces the default endpoint for exposing probes endpoint. | `/healthz`
`podMonitor.create` | If `true`, create a PodMonitor | `false`
`podMonitor.interval` | Prometheus scrape interval | `30s`
`podMonitor.sampleLimit` | Number of scraped samples accepted | `5000`
`podMonitor.labels` | Additional PodMonitor metadata labels | `{}`
`podMonitor.namespace` | Override podMonitor Helm release namespace | `{{ .Release.Namespace }}`
`emitKubernetesEvents` | If `true`, Kubernetes events will be emitted when interruption events are received and when actions are taken on Kubernetes nodes. In IMDS Processor mode a default set of annotations with all the node metadata gathered from IMDS will be attached to each event. More information [here](https://github.com/aws/aws-node-termination-handler/blob/main/docs/kubernetes_events.md) | `false`
`kubernetesExtraEventsAnnotations` | A comma-separated list of `key=value` extra annotations to attach to all emitted Kubernetes events. Example: `first=annotation,sample.annotation/number=two"` | None

### AWS Node Termination Handler - Queue-Processor Mode Configuration

Parameter | Description | Default
--- | --- | ---
`enableSqsTerminationDraining` | If true, this turns on queue-processor mode which drains nodes when an SQS termination event is received| `false`
`queueURL` | Listens for messages on the specified SQS queue URL | None
`awsRegion` | If specified, use the AWS region for AWS API calls, else NTH will try to find the region through AWS_REGION env var, IMDS, or the specified queue URL | ``
`checkASGTagBeforeDraining` | If true, check that the instance is tagged with "aws-node-termination-handler/managed" as the key before draining the node | `true`
`managedAsgTag` | The tag to ensure is on a node if checkASGTagBeforeDraining is true | `aws-node-termination-handler/managed`
`workers` | The maximum amount of parallel event processors | `10`
`replicas` | The number of replicas in the NTH deployment when using queue-processor mode (NOTE: increasing replicas may cause duplicate webhooks since NTH pods are stateless)
 | `1`

### AWS Node Termination Handler - IMDS Mode Configuration

Parameter | Description | Default
--- | --- | ---
`enableScheduledEventDraining` | [EXPERIMENTAL] If true, drain nodes before the maintenance window starts for an EC2 instance scheduled event | `false`
`enableSpotInterruptionDraining` | If true, drain nodes when the spot interruption termination notice is received | `true`
`enableRebalanceMonitoring` | If true, cordon nodes when the rebalance recommendation notice is received | `false`
`enableRebalanceDraining` | If true, drain nodes when the rebalance recommendation notice is received | `false`
`useHostNetwork` | If `true`, enables `hostNetwork` for the Linux DaemonSet. NOTE: setting this to `false` may cause issues accessing IMDSv2 if your account is not configured with an IP hop count of 2 | `true`

### Kubernetes Configuration

Parameter | Description | Default
--- | --- | ---
`image.repository` | image repository | `public.ecr.aws/aws-ec2/aws-node-termination-handler`
`image.tag` | image tag | `<VERSION>`
`image.pullPolicy` | image pull policy | `IfNotPresent`
`image.pullSecrets` | image pull secrets (for private docker registries) | `[]`
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
`securityContext.runAsUserID` | User ID to run the container | `1000`
`securityContext.runAsGroupID` | Group ID to run the container | `1000`
`nodeSelectorTermsOs` | Operating System Node Selector Key | >=1.14: `kubernetes.io/os`, <1.14: `beta.kubernetes.io/os`
`nodeSelectorTermsArch` | CPU Architecture Node Selector Key | >=1.14: `kubernetes.io/arch`, <1.14: `beta.kubernetes.io/arch`
`targetNodeOs` | Space separated list of node OS's to target, e.g. "linux", "windows", "linux windows".  Note: Windows support is experimental. | `"linux"`
`updateStrategy` | Update strategy for the all DaemonSets (Linux and Windows) | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`
`linuxUpdateStrategy` | Update strategy for the Linux DaemonSet | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`
`windowsUpdateStrategy` | Update strategy for the Windows DaemonSet | `type=RollingUpdate,rollingUpdate.maxUnavailable=1`

### Testing Configuration (NOT RECOMMENDED FOR PROD DEPLOYMENTS)

Parameter | Description | Default
--- | --- | ---
`procUptimeFile` | (Used for Testing) Specify the uptime file | `/proc/uptime`
`awsEndpoint` | (Used for testing) If specified, use the AWS endpoint to make API calls | None
`awsSecretAccessKey` | (Used for testing) Pass-thru env var | None
`awsAccessKeyID` | (Used for testing) Pass-thru env var | None
`dryRun` | If true, only log if a node would be drained | `false`

## Metrics endpoint consideration

NTH in IMDS mode runs as a DaemonSet w/ `host_networking=true` by default. If the prometheus server is enabled, nothing else will be able to bind to the configured port (by default `:9092`) in the root network namespace. Therefore, it will need to have a firewall/security group configured on the nodes to block access to the `/metrics` endpoint.

You can switch NTH in IMDS mode to run w/ `host_networking=false`, but you will need to make sure that IMDSv1 is enabled or IMDSv2 IP hop count will need to be incremented to 2. https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html
