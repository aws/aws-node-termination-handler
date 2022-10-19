# AWS Node Termination Handler

AWS Node Termination Handler Helm chart for Kubernetes. For more information on this project see the project repo at [github.com/aws/aws-node-termination-handler](https://github.com/aws/aws-node-termination-handler).

## Prerequisites

- _Kubernetes_ >= 1.16

## Installing the Chart

Before you can install the chart you will need to add the `eks` repo to [Helm](https://helm.sh/).

```shell
helm repo add eks https://aws.github.io/eks-charts/
```

### Configuration

* `annotations` - Annotation names and values to add to objects in the Helm release. Default: `{}`.
* `aws.region` - AWS region name (e.g. "us-east-1") to use when making API calls. Default: `""`.
* `controller.env` - List of environment variables to set in the controller container. See [core/v1 Pod.spec.containers.env](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#container-v1-core) Default: `[]`.
* `controller.image` - Image repository for the controller.
* `controller.logLevel` - Override the global logging level for the controller container. Default: `""`.
* `controller.resources` - Resource requests and limits for controller container. See [core/v1 ResourceRequests](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#resourcerequirements-v1-core) for further information. Default: `{"requests":{"cpu": 1, "memory": "1Gi"}, "limits":{"cpu": 1, "memory": "1Gi"}}`
* `controller.securityContext` - Controller container security context configuration. See [core/v1 Pod.spec.securityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podsecuritycontext-v1-core) for further information. Default: `{}`.
* `fullnameOverride` - Override the Helm release name. Name will be truncated if longer than 63 characters. Default is generated from the release name and chart name.
* `imagePullPolicy` - Policy for when to pull images. See [core/v1 Container.imagePullPolicy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#container-v1-core) for further information. Default: `"IfNotPresent"`.
* `imagePullSecrets` - List of secrets to use when pulling images. See [apps/v1 Deployment.spec.template.spec.imagePullSecrets](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podspec-v1-core) for further information. Default: `[]`.
* `labels` - Label names and values to add to objects in the Helm release. Default: `{}`.
* `logging.development` - Enable "debug mode" in logging module. May be useful during development. Default: `false`.
* `logging.disableCaller` - Disable annotating log messages with calling function's file name and line number. Default: `true`.
* `logging.disableStacktrace` - Disable stacktrace captures for all message levels. Default: `true`.
* `logging.encoding` - Logging module encoding mode. Possible values: `console`, `json`. Default: `console`.
* `logging.encoderConfig.callerKey` - Name of the caller field. Default: `"caller"`.
* `logging.encoderConfig.levelEncoder` - Level encoder name. Possible values: `capital`, `capitalColor`, `color`; otherwise the level name will be encoded as lowercase. Default: `"capital"`.
* `logging.encoderConfig.levelKey` - Name of the level field. Default: `"level"`.
* `logging.encoderConfig.messageKey` - Name of the message field. Default: `"message"`.
* `logging.encoderConfig.nameKey` - Name of the logger name field. Default: `"logger"`.
* `logging.encoderConfig.stacktraceKey` - Name of the stacktrace field. Default: `"stacktrace"`.
* `logging.encoderConfig.timeEncoder` - Time encoder name. Possible values: `iso8601`, `millis`, `nano`, `rfc3339`, `rfc3339nano`; otherwise the time will be encoded in epoch format. Default: `"iso8601"`.
* `logging.encoderConfig.timeKey` - Name of the time field. Default: `"time"`.
* `logging.errorOutputPaths` - List of paths to output internal errors from the logging module. Possible values: `stderr`, `stdout`; otherwise a valid file path. Default: `["stderr"]`.
* `logging.level` - Minimum message level to include in the log. Possible values: `debug`, `info`, `warn`, `error`, `panic`, `fatal`. Default: `info`.
* `logging.outputPaths` - List of additional output paths. Possible values: `stderr`, `stdout`; otherwise a valid file path. Default: `["stdout"]`.
* `logging.sampling.initial` - Limit of initial messages per second to accept. Default: `100`.
* `logging.sampling.thereafter` - Limit of messages per second to accept after initial phase. Default: `100`.
* `nameOverride` - Override the Helm chart name. Name will be truncated if longer than 63 characters. Default: `.Chart.Name`.
* `pod.annotations` - Annotation to apply to deployed pod. Default: `{}`.
* `pod.hostNetwork` - Request host network for pod. See [core/v1 Pod.spec.hostNetwork](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podspec-v1-core) for futher information. Default: `false`.
* `pod.labels` - Labels to apply to deployed pod. Default: `{}`.
* `pod.nodeSelector` - Node selector labels. Default: `{"kubernetes.io/os": "linux"}`
* `pod.priorityClassName` - Pod priority class. See [core/v1 Pod.spec.priorityClassName](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podspec-v1-core) for futher information. Default: `"system-cluster-critical"`.
* `pod.replicas` - Number of instances to create. Default: `1`.
* `pod.securityContext` - Pod security context configuration. See documentation for [core/v1 Pod.spec.securityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podsecuritycontext-v1-core) for available properties. Default: `{"fsGroup": 1000}`.
* `pod.updateStrategy` - Deployment update strategy configuration. See documentation for [apps/v1 Deployment.spec.strategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#deploymentstrategy-v1-apps) for available properties. Default: `{"type": "Recreate"}`.
* `rbac.create` - Enable creation of RBAC objects. Helm release may fail is RBAC objects already exist. Default: `true`.
* `serviceAccount.annotations` - Annotation names and values to add to service account. Default: `{}`.
* `serviceAccount.create` - Enable creation of service account. Helm release may fail if service account already exists. Default: `true`.
* `serviceAccount.name` - Name of the service account. If `serviceAccount.create` is enabled then the default will be generated from the release name and chart name. If `serviceAccount.create` is disabled then the default is `"default"`.
* `webhook.env` - List of environment variables to set in the webhook container. See [core/v1 Pod.spec.containers.env](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#container-v1-core) Default: `[]`.
* `webhook.image` - Image repository for the webhook controller.
* `webhook.logLevel` - Override the global logging level for the webhook container. Default: `""`.
* `webhook.port` - List on port. Default: `8443`.
* `webhook.resources` - Resource requests and limits for webhook container. See [core/v1 ResourceRequests](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#resourcerequirements-v1-core) for further information. Default: `{"requests":{"cpu": 1, "memory": "1Gi"}, "limits":{"cpu": 1, "memory": "1Gi"}}`
* `webhook.securityContext` - Controller container security context configuration. See [core/v1 Pod.spec.securityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/#podsecuritycontext-v1-core) for further information. Default: `{}`.
