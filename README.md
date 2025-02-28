<h1>AWS Node Termination Handler</h1>

<h4>Gracefully handle EC2 instance shutdown within Kubernetes</h4>

<p>
  <a href="https://github.com/kubernetes/kubernetes/releases">
    <img src="https://img.shields.io/badge/Kubernetes-%3E%3D%201.23-brightgreen" alt="kubernetes">
  </a>
  <a href="https://golang.org/doc/go1.22">
    <img src="https://img.shields.io/github/go-mod/go-version/aws/aws-node-termination-handler?color=blueviolet" alt="go-version">
  </a>
  <a href="https://opensource.org/licenses/Apache-2.0">
    <img src="https://img.shields.io/badge/License-Apache%202.0-ff69b4.svg" alt="license">
  </a>
  <a href="https://codecov.io/gh/aws/aws-node-termination-handler">
    <img src="https://img.shields.io/codecov/c/github/aws/aws-node-termination-handler" alt="build-status">
  </a>
  <a href="https://gallery.ecr.aws/aws-ec2/aws-node-termination-handler">
    <img src="https://img.shields.io/docker/pulls/amazon/aws-node-termination-handler" alt="docker-pulls">
  </a>
    <a href="https://github.com/aws/aws-node-termination-handler/workflows">
    <img src="https://img.shields.io/github/workflow/status/aws/aws-node-termination-handler/Build%20and%20Test?label=Builds%20%26%20Tests">
  </a>
</p>

<div>
<hr>
</div>


## Project Summary

This project ensures that the Kubernetes control plane responds appropriately to events that can cause your EC2 instance to become unavailable, such as [EC2 maintenance events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html), [EC2 Spot interruptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-interruptions.html), [ASG Scale-In](https://docs.aws.amazon.com/autoscaling/ec2/userguide/AutoScalingGroupLifecycle.html#as-lifecycle-scale-in), ASG AZ Rebalance, and EC2 Instance Termination via the API or Console.  If not handled, your application code may not stop gracefully, take longer to recover full availability, or accidentally schedule work to nodes that are going down.

The aws-node-termination-handler (NTH) can operate in two different modes: Instance Metadata Service (IMDS) or the Queue Processor.

The aws-node-termination-handler **[Instance Metadata Service](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-metadata.html) Monitor** will run a small pod on each host to perform monitoring of IMDS paths like `/spot` or `/events` and react accordingly to drain and/or cordon the corresponding node.

The aws-node-termination-handler **Queue Processor** will monitor an SQS queue of events from Amazon EventBridge for ASG lifecycle events, EC2 status change events, Spot Interruption Termination Notice events, and Spot Rebalance Recommendation events. When NTH detects an instance is going down, we use the Kubernetes API to cordon the node to ensure no new work is scheduled there, then drain it, removing any existing work. The termination handler **Queue Processor** requires AWS IAM permissions to monitor and manage the SQS queue and to query the EC2 API.

You can run the termination handler on any Kubernetes cluster running on AWS, including self-managed clusters and those created with Amazon [Elastic Kubernetes Service](https://docs.aws.amazon.com/eks/latest/userguide/what-is-eks.html). If you're using [EKS managed node groups](https://docs.aws.amazon.com/eks/latest/userguide/managed-node-groups.html), you don't need the aws-node-termination-handler.

## Major Features

Both modes (IMDS and Queue Processor) monitor for events affecting your EC2 instances, but each supports different types of events. Both modes have the following:

- Helm installation and event configuration support
- Webhook feature to send shutdown or restart notification messages
- Unit & integration tests

### Instance Metadata Service (IMDS) Processor
Must be deployed as a Kubernetes **DaemonSet**.

- Monitors EC2 Instance Metadata for:
   - [Spot Instance Termination Notifications](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-instance-termination-notices.html)
   - [Scheduled Events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html)
   - [Instance Rebalance Recommendations](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/rebalance-recommendations.html)
   - [Autoscaling Group Target Lifecycle State changes](https://docs.aws.amazon.com/autoscaling/ec2/userguide/retrieving-target-lifecycle-state-through-imds.html)

#### IMDS Processor with ASG Target Lifecycle State change
Please note that IMDS does **not** support lifecycle *hooks*, but it does support lifecycle *state* change. When using IMDS mode with the ASG target lifecycle state, ASG will update instance metadata to be **Terminated** before it terminates the node. NTH will monitor the path latest/meta-data/autoscaling/target-lifecycle-state for changes and will cordon and drain when the target state is set to **Terminated**.

### Queue Processor
Must be deployed as a Kubernetes **Deployment**. Also requires some **additional infrastructure setup** (including SQS queue, EventBridge rules).

- Monitors an SQS Queue for:
   - Spot Instance Termination Notifications
   - Scheduled Events (via AWS Health)
   - Instance Rebalance Recommendations
   - ASG Termination Lifecycle Hooks to handle the following:
     - [ASG Scale-In](https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html)
     - [Availability Zone Rebalance](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-instance-termination.html#:~:text=are%20replaced%20first.-,Availability%20Zone%20rebalancing,-Amazon%20EC2%20Auto)
     - [Unhealthy Instances](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-health-checks.html), and more
   - [Instance State Change events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instance-state-changes.html)

We can use the **Queue Processor** for both ASG Lifecycle Termination Hooks and Instance State Change Events for termination of nodes. Below listed are the details on how AWS EC2 takes actions for graceful shutdowns. You can pick one that is best suitable for your use, based on the configuration and workloads.

#### Queue Processor with ASG Lifecycle Hooks
When using the ASG Lifecycle Hooks, ASG first sends the lifecycle action notification then waits until it has been completed or times out. This allows time for NTH to receive the notification via SQS, cordon and drain the node, and then complete the lifecycle action. Once the ASG receives the completion it then instructs EC2 to terminate the instance.

#### Queue Processor with Instance State Change Events
When using the EC2 Console or EC2 API to terminate the instance, a state-change notification is sent and the instance termination is started. EC2 does not wait for a "continue" signal before beginning to terminate the instance. When you terminate an EC2 instance, it should trigger a graceful operating system shutdown which will send a SIGTERM to the kubelet, which will in-turn start shutting down pods by propagating that SIGTERM to the containers on the node. If the containers do not shut down by the kubelet's `podTerminationGracePeriod (k8s default is 30s)`, then it will send a SIGKILL to forcefully terminate the containers. Setting the `podTerminationGracePeriod` to a max of 90sec (probably a bit less than that) will delay the termination of pods, which helps in graceful shutdown.

#### Issuing Lifecycle Heartbeats

You can set NTH to send heartbeats to ASG in Queue Processor mode. This allows for a much longer grace period (up to 48 hours) for termination than the maximum heartbeat timeout of two hours. The feature is useful when pods require long time to drain or when you need a shorter heartbeat timeout with a longer grace period.

##### How it works

- When NTH receives an ASG lifecycle termination event, it starts sending heartbeats to ASG to renew the heartbeat timeout associated with the ASG's termination lifecycle hook.
- The heartbeat timeout acts as a timer that starts when the termination event begins.
- Before the timeout reaches zero, the termination process is halted at the `Terminating:Wait` stage.
- By issuing heartbeats, graceful termination duration can be extended up to 48 hours, limited by the global timeout.

##### How to use

- Configure a termination lifecycle hook on ASG (required). Set the heartbeat timeout value to be longer than the `Heartbeat Interval`. Each heartbeat signal resets this timeout, extending the duration that an instance remains in the `Terminating:Wait` state. Without this lifecycle hook, the instance will terminate immediately when termination event occurs.
- Configure `Heartbeat Interval` (required) and `Heartbeat Until` (optional). NTH operates normally without heartbeats if neither value is set. If only the interval is specified, `Heartbeat Until` defaults to 172800 seconds (48 hours) and heartbeats will be sent. `Heartbeat Until` must be provided with a valid `Heartbeat Interval`, otherwise NTH will fail to start. Any invalid values (wrong type or out of range) will also prevent NTH from starting.

##### Configurations
###### `Heartbeat Interval` (Required)
- Time period between consecutive heartbeat signals (in seconds)
- Specifying this value triggers heartbeat
- Range: 30 to 3600 seconds (30 seconds to 1 hour)
- Flag for custom resource definition by *.yaml / helm: `heartbeatInterval`
- CLI flag: `heartbeat-interval`
- Default value: X

###### `Heartbeat Until` (Optional)
- Duration over which heartbeat signals are sent (in seconds)
- Must be provided with a valid `Heartbeat Interval`
- Range: 60 to 172800 seconds (1 minute to 48 hours)
- Flag for custom resource definition by *.yaml / helm: `heartbeatUntil`
- CLI flag: `heartbeat-until`
- Default value: 172800 (48 hours)

###### Example Case

- `Heartbeat Interval`: 1000 seconds
- `Heartbeat Until`: 4500 seconds
- `Heartbeat Timeout`: 3000 seconds 

| Time (s) | Event | Heartbeat Timeout (HT) | Heartbeat Until (HU) | Action |
|----------|-------------|------------------|----------------------|--------|
| 0        | Start       | 3000            | 4500                  | Termination Event Received |
| 1000     | HB1 Issued  | 2000 -> 3000    | 3500                  | Send Heartbeat |
| 2000     | HB2 Issued  | 2000 -> 3000    | 2500                  | Send Heartbeat |
| 3000     | HB3 Issued  | 2000 -> 3000    | 1500                  | Send Heartbeat |
| 4000     | HB4 Issued  | 2000 -> 3000    | 500                   | Send Heartbeat |
| 4500     | HB Expires  | 2500            | 0                     | Stop Heartbeats |
| 7000     | Termination | -               | -                     | Instance Terminates |

Note: The instance can terminate earlier if its pods finish draining and are ready for termination.

##### Example Helm Command

```sh
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set heartbeatInterval=1000 \
  --set heartbeatUntil=4500 \
  // other inputs..
```

##### Important Notes

- Be aware of global timeout. Instances cannot remain in a wait state indefinitely. The global timeout is 48 hours or 100 times the heartbeat timeout, whichever is smaller. This is the maximum amount of time that you can keep an instance in `terminating:wait` state.
- Lifecycle heartbeats are only supported in Queue Processor mode. Setting `enableSqsTerminationDraining=false` and specifying heartbeat flags is prevented in Helm. Directly editing deployment settings to bypass this will cause NTH to fail.
- The heartbeat interval should be sufficiently shorter than the heartbeat timeout. There's a time gap between instance startup and NTH initialization. Setting the interval just slightly smaller than or equal to the timeout causes the heartbeat timeout to expire before the first heartbeat is issued. Provide adequate buffer time for NTH to complete initialization.
- Issuing heartbeats is part of the termination process. The maximum number of instances that NTH can handle termination concurrently is limited by the number of workers. This implies that heartbeats can only be issued for up to the number of instances specified by the `workers` flag simultaneously.

### Which one should I use?
|                    Feature                    | IMDS Processor | Queue Processor |
| :-------------------------------------------: | :------------: | :-------------: |
| Spot Instance Termination Notifications (ITN) |       ✅        |        ✅        |
|               Scheduled Events                |       ✅        |        ✅        |
|       Instance Rebalance Recommendation       |       ✅        |        ✅        |
|        ASG Termination Lifecycle Hooks        |       ❌        |        ✅        |
|     ASG Termination Lifecycle State Change    |       ✅        |        ❌        |
|         AZ Rebalance Recommendation           |       ❌        |        ✅        |
|         Instance State Change Events          |       ❌        |        ✅        |
|          Issue Lifecycle Heartbeats           |       ❌        |        ✅        |

### Kubernetes Compatibility

|                                      NTH Release                                      | K8s v1.32 | K8s v1.31 | K8s v1.30 | K8s v1.29 | K8s v1.28 | K8s v1.27 | K8s v1.26 | K8s v1.25 |
| :-----------------------------------------------------------------------------------: | :-------: | :-------: | :-------: | :-------: | :-------: | :-------: | :-------: | :-------: |
|  [v1.25.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.25.0)  |     ✅    |     ✅    |     ✅    |     ✅    |     ❌    |     ❌    |     ❌    |     ❌    |
|  [v1.24.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.24.0)  |     ❌    |     ✅    |     ✅    |     ✅    |     ❌    |     ❌    |     ❌    |     ❌    |
|  [v1.23.1](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.23.1)  |     ❌    |     ✅    |     ✅    |     ✅    |     ❌    |     ❌    |     ❌    |     ❌    |
|  [v1.23.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.23.0)  |     ❌    |     ❌    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |
|  [v1.22.1](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.22.1)  |     ❌    |     ❌    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |
|  [v1.22.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.22.0)  |     ❌    |     ❌    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |
|  [v1.21.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.21.0)  |     ❌    |     ❌    |     ❌    |     ✅    |     ✅    |     ✅    |     ✅    |     ✅    |
|  [v1.20.0](https://github.com/aws/aws-node-termination-handler/releases/tag/v1.20.0)  |     ❌    |     ❌    |     ❌    |     ❌    |     ✅    |     ✅    |     ✅    |     ✅    |

A ✅ indicates that a specific aws-node-termination-handler release has been tested with a specific Kubernetes version. A ❌ indicates that a specific aws-node-termination-handler release has not been tested with a specific Kubernetes version.

## Installation and Configuration

The aws-node-termination-handler can operate in two different modes: IMDS Processor and Queue Processor. The `enableSqsTerminationDraining` helm configuration key or the `ENABLE_SQS_TERMINATION_DRAINING` environment variable are used to enable the Queue Processor mode of operation. If `enableSqsTerminationDraining` is set to true, then IMDS paths will NOT be monitored. If the `enableSqsTerminationDraining` is set to false, then IMDS Processor Mode will be enabled. Queue Processor Mode and IMDS Processor Mode cannot be run at the same time.

IMDS Processor Mode allows for a fine-grained configuration of IMDS paths that are monitored. There are currently 3 paths supported that can be enabled or disabled by using the following helm configuration keys:
 - `enableSpotInterruptionDraining`
 - `enableRebalanceMonitoring`
 - `enableScheduledEventDraining`

By default, IMDS mode will only Cordon in response to a Rebalance Recommendation event (all other events are Cordoned and Drained). Cordon is the default for a rebalance event because it's not known if an ASG is being utilized and if that ASG is configured to replace the instance on a rebalance event. If you are using an ASG w/ rebalance recommendations enabled, then you can set the `enableRebalanceDraining` flag to true to perform a Cordon and Drain when a rebalance event is received.

Rebalance Recommendation is an early indicator to notify the Spot Instances that they can be interrupted soon. Node Termination Handler supports AZ Rebalance Recommendation only in Queue Processor mode using ASG Lifecycle Hooks. For AZ rebalances the instances are just terminated, using Lifecycle Hooks and EventBridge rule for `EC2 Instance-terminate Lifecycle Action` we can handle OD Instances.

The `enableSqsTerminationDraining` must be set to false for these configuration values to be considered.

The Queue Processor Mode does not allow for fine-grained configuration of which events are handled through helm configuration keys. Instead, you can modify your Amazon EventBridge rules to not send certain types of events to the SQS Queue so that NTH does not process those events. All events when operating in Queue Processor mode are Cordoned and Drained unless the `cordon-only` flag is set to true.

The `enableSqsTerminationDraining` flag turns on Queue Processor Mode. When Queue Processor Mode is enabled, IMDS mode will be disabled, even if you explicitly enabled any of the IMDS configuration keys. NTH cannot respond to queue events AND monitor IMDS paths. In this case, it is safe to disable IMDS for the NTH pod.

<details opened>
<summary>AWS Node Termination Handler - IMDS Processor</summary>
<br>

### Installation and Configuration

The termination handler DaemonSet installs into your cluster a [ServiceAccount](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/), [ClusterRole](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), [ClusterRoleBinding](https://kubernetes.io/docs/reference/access-authn-authz/rbac/), and a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). All four of these Kubernetes constructs are required for the termination handler to run properly.

#### Pod Security Admission

When using Kubernetes [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) it is recommended to assign the `[privileged](https://kubernetes.io/docs/concepts/security/pod-security-standards/#privileged)` level.

#### Kubectl Apply

You can use kubectl to directly add all of the above resources with the default configuration into your cluster.

```
kubectl apply -f https://github.com/aws/aws-node-termination-handler/releases/download/v1.25.0/all-resources.yaml
```

For a full list of releases and associated artifacts see our [releases page](https://github.com/aws/aws-node-termination-handler/releases).

#### Helm

The easiest way to configure the various options of the termination handler is via [helm](https://helm.sh/). The chart for this project is hosted in [helm/aws-node-termination-handler](https://gallery.ecr.aws/aws-ec2/helm/aws-node-termination-handler)

To get started you need to authenticate your helm client

```
aws ecr-public get-login-password \
  --region us-east-1 | helm registry login \
  --username AWS \
  --password-stdin public.ecr.aws
```

Once that is complete you can install the termination handler. We've provided some sample setup options below. Make sure to replace CHART_VERSION with the version you want to install.

Zero Config:

```sh
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

Enabling Features:

```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSpotInterruptionDraining="true" \
  --set enableRebalanceMonitoring="true" \
  --set enableScheduledEventDraining="false" \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

The `enable*` configuration flags above enable or disable IMDS monitoring paths.

Running Only On Specific Nodes:

```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set nodeSelector.lifecycle=spot \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

Webhook Configuration:

```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set webhookURL=https://hooks.slack.com/services/YOUR/SLACK/URL \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

Alternatively, pass Webhook URL as a Secret:

```
WEBHOOKURL_LITERAL="webhookurl=https://hooks.slack.com/services/YOUR/SLACK/URL"

kubectl create secret -n kube-system generic webhooksecret --from-literal=$WEBHOOKURL_LITERAL
```
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set webhookURLSecretName=webhooksecret \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

For a full list of configuration options see our [Helm readme](https://github.com/aws/aws-node-termination-handler/blob/v1.25.0/config/helm/aws-node-termination-handler#readme).

</details>


<details closed>
<summary>AWS Node Termination Handler - Queue Processor (requires AWS IAM Permissions)</summary>

<br>

### Infrastructure Setup

The termination handler requires some infrastructure prepared before deploying the application. In a multi-cluster environment, you will need to repeat the following steps for each cluster.

You'll need the following AWS infrastructure components:

1. Amazon Simple Queue Service (SQS) Queue
2. AutoScaling Group Termination Lifecycle Hook
3. Instance Tagging
4. Amazon EventBridge Rule
5. IAM Role for the aws-node-termination-handler Queue Processing Pods

Optional AWS infrastructure components:
1. AutoScaling Group Launch Lifecycle Hook

#### 1. Create an SQS Queue:

Here is the AWS CLI command to create an SQS queue to hold termination events from ASG and EC2, although this should really be configured via your favorite infrastructure-as-code tool like CloudFormation (template [here](docs/cfn-template.yaml)) or Terraform:

```
## Queue Policy
QUEUE_POLICY=$(cat <<EOF
{
    "Version": "2012-10-17",
    "Id": "MyQueuePolicy",
    "Statement": [{
        "Effect": "Allow",
        "Principal": {
            "Service": ["events.amazonaws.com", "sqs.amazonaws.com"]
        },
        "Action": "sqs:SendMessage",
        "Resource": [
            "arn:aws:sqs:${AWS_REGION}:${ACCOUNT_ID}:${SQS_QUEUE_NAME}"
        ]
    }]
}
EOF
)

## make sure the queue policy is valid JSON
echo "$QUEUE_POLICY" | jq .

## Save queue attributes to a temp file
cat << EOF > /tmp/queue-attributes.json
{
  "MessageRetentionPeriod": "300",
  "Policy": "$(echo $QUEUE_POLICY | sed 's/\"/\\"/g' | tr -d -s '\n' " ")",
  "SqsManagedSseEnabled": "true"
}
EOF

aws sqs create-queue --queue-name "${SQS_QUEUE_NAME}" --attributes file:///tmp/queue-attributes.json
```

If you are sending Lifecycle termination events from ASG directly to SQS, instead of through EventBridge, then you will also need to create an IAM service role to give Amazon EC2 Auto Scaling access to your SQS queue. Please follow [these linked instructions to create the IAM service role: link.](https://docs.aws.amazon.com/autoscaling/ec2/userguide/configuring-lifecycle-hook-notifications.html#sqs-notifications)
Note the ARNs for the SQS queue and the associated IAM role for Step 2.

There are some caveats when using [server side encryption with SQS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-server-side-encryption.html):
* using [SSE-KMS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-configure-sse-existing-queue.html) with a [customer managed key](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#key-mgmt) requires [changing the KMS key policy](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-troubleshooting.html#eb-sqs-encrypted) to allow EventBridge to publish events to SQS.
* using [SSE-KMS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-configure-sse-existing-queue.html) with an [AWS managed key](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#key-mgmt) is not supported as the KMS key policy can't be updated to allow EventBridge to publish events to SQS.
* using [SSE-SQS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-configure-sqs-sse-queue.html) doesn't require extra setup and works out of the box as SQS queues without encryption at rest.

#### 2. Create an ASG Termination Lifecycle Hook:

Here is the AWS CLI command to create a termination lifecycle hook on an existing ASG when using EventBridge, although this should really be configured via your favorite infrastructure-as-code tool like CloudFormation or Terraform:

```
aws autoscaling put-lifecycle-hook \
  --lifecycle-hook-name=my-k8s-term-hook \
  --auto-scaling-group-name=my-k8s-asg \
  --lifecycle-transition=autoscaling:EC2_INSTANCE_TERMINATING \
  --default-result=CONTINUE \
  --heartbeat-timeout=300
```

If you want to avoid using EventBridge and instead send ASG Lifecycle events directly to SQS, instead use the following command, using the ARNs from Step 1:

```
aws autoscaling put-lifecycle-hook \
  --lifecycle-hook-name=my-k8s-term-hook \
  --auto-scaling-group-name=my-k8s-asg \
  --lifecycle-transition=autoscaling:EC2_INSTANCE_TERMINATING \
  --default-result=CONTINUE \
  --heartbeat-timeout=300 \
  --notification-target-arn <your queue ARN here> \
  --role-arn <your SQS access role ARN here>
```

#### 3. Tag the Instances:

By default the aws-node-termination-handler will only manage terminations for instances tagged with `key=aws-node-termination-handler/managed`.
The value of the key does not matter.

To tag ASGs and propagate the tags to your instances (recommended):
```
aws autoscaling create-or-update-tags \
  --tags ResourceId=my-auto-scaling-group,ResourceType=auto-scaling-group,Key=aws-node-termination-handler/managed,Value=,PropagateAtLaunch=true
```

To tag an individual EC2 instance:
```
aws ec2 create-tags \
    --resources i-1234567890abcdef0 \
    --tags 'Key="aws-node-termination-handler/managed",Value='
```

Tagging your EC2 instances in this way is helpful if you only want aws-node-termination-handler to manage the lifecycle of instances in certain ASGs. For example, if your account also has other ASGs that do not contain Kubernetes nodes, this tagging mechanism will ensure that NTH does not manage the lifecycle of any instances in those non-Kubernetes ASGs.

However, if the only ASGs in your account are for your Kubernetes cluster, then you can turn off the tag check by setting the flag `--check-tag-before-draining=false` or environment variable `CHECK_TAG_BEFORE_DRAINING=false`.

You can also control what resources NTH manages by adding the resource ARNs to your Amazon EventBridge rules.

Take a look at the docs on how to [create rules that only manage certain ASGs](https://docs.aws.amazon.com/autoscaling/ec2/userguide/cloud-watch-events.html), and read about all the [supported ASG events](https://docs.aws.amazon.com/autoscaling/ec2/userguide/ec2-auto-scaling-event-reference.html).

#### 4. Create Amazon EventBridge Rules

You may skip this step if sending events from ASG to SQS directly.

If we use ASG with capacity-rebalance enabled on ASG, then we do not need Spot and Rebalance events enabled with EventBridge. ASG will send a termination lifecycle hook for spot interrruptions while it's launching a new instance and for Rebalance events ASG will send a termination lifecycle hook after it brings a new node in the ASG.

If we use ASG without capacity-rebalance enabled, then spot interruptions will cause a termination lifecycle hook after the interruption occurs but not while launching the new instance.

Here are AWS CLI commands to create Amazon EventBridge rules so that ASG termination events, Spot Interruptions, Instance state changes, Rebalance Recommendations, and AWS Health Scheduled Changes are sent to the SQS queue created in the previous step. This should really be configured via your favorite infrastructure-as-code tool like CloudFormation (template [here](docs/cfn-template.yaml)) or Terraform:

```
aws events put-rule \
  --name MyK8sASGTermRule \
  --event-pattern "{\"source\":[\"aws.autoscaling\"],\"detail-type\":[\"EC2 Instance-terminate Lifecycle Action\"]}"

aws events put-targets --rule MyK8sASGTermRule \
  --targets "Id"="1","Arn"="arn:aws:sqs:us-east-1:123456789012:MyK8sTermQueue"

aws events put-rule \
  --name MyK8sSpotTermRule \
  --event-pattern "{\"source\": [\"aws.ec2\"],\"detail-type\": [\"EC2 Spot Instance Interruption Warning\"]}"

aws events put-targets --rule MyK8sSpotTermRule \
  --targets "Id"="1","Arn"="arn:aws:sqs:us-east-1:123456789012:MyK8sTermQueue"

aws events put-rule \
  --name MyK8sRebalanceRule \
  --event-pattern "{\"source\": [\"aws.ec2\"],\"detail-type\": [\"EC2 Instance Rebalance Recommendation\"]}"

aws events put-targets --rule MyK8sRebalanceRule \
  --targets "Id"="1","Arn"="arn:aws:sqs:us-east-1:123456789012:MyK8sTermQueue"

aws events put-rule \
  --name MyK8sInstanceStateChangeRule \
  --event-pattern "{\"source\": [\"aws.ec2\"],\"detail-type\": [\"EC2 Instance State-change Notification\"]}"

aws events put-targets --rule MyK8sInstanceStateChangeRule \
  --targets "Id"="1","Arn"="arn:aws:sqs:us-east-1:123456789012:MyK8sTermQueue"

aws events put-rule \
  --name MyK8sScheduledChangeRule \
  --event-pattern "{\"source\": [\"aws.health\"],\"detail-type\": [\"AWS Health Event\"],\"detail\": {\"service\": [\"EC2\"],\"eventTypeCategory\": [\"scheduledChange\"]}}"

aws events put-targets --rule MyK8sScheduledChangeRule \
  --targets "Id"="1","Arn"="arn:aws:sqs:us-east-1:123456789012:MyK8sTermQueue"
```

#### 5. Create an IAM Role for the Pods

There are many different ways to allow the aws-node-termination-handler pods to assume a role:

1. [Amazon EKS IAM Roles for Service Accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
2. [IAM Instance Profiles for EC2](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_use_switch-role-ec2_instance-profiles.html)
3. [Kiam](https://github.com/uswitch/kiam)
4. [kube2iam](https://github.com/jtblin/kube2iam)

IAM Policy for aws-node-termination-handler Deployment:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "autoscaling:CompleteLifecycleAction",
                "autoscaling:DescribeAutoScalingInstances",
                "autoscaling:DescribeTags",
                "ec2:DescribeInstances",
                "sqs:DeleteMessage",
                "sqs:ReceiveMessage"
            ],
            "Resource": "*"
        }
    ]
}
```

#### 1. Handle ASG Instance Launch Lifecycle Notifications (optional):

NTH can monitor for new instances launched by an ASG and notify the ASG when the instance is available in the EKS cluster.

NTH will need to receive notifications of new instance launches within the ASG.  We can add a lifecycle hook to the ASG that will send instance launch notifications via EventBridge:

```
aws autoscaling put-lifecycle-hook \
  --lifecycle-hook-name=my-k8s-launch-hook \
  --auto-scaling-group-name=my-k8s-asg \
  --lifecycle-transition=autoscaling:EC2_INSTANCE_LAUNCHING \
  --default-result="ABANDON" \
  --heartbeat-timeout=300
```

Alternatively, ASG can send the instance launch notification directly to an SQS Queue:

```
aws autoscaling put-lifecycle-hook \
  --lifecycle-hook-name=my-k8s-launch-hook \
  --auto-scaling-group-name=my-k8s-asg \
  --lifecycle-transition=autoscaling:EC2_INSTANCE_LAUNCHING \
  --default-result="ABANDON" \
  --heartbeat-timeout=300 \
  --notification-target-arn <your queue ARN here> \
  --role-arn <your SQS access role ARN here>
```    

When NTH receives a launch notification, it will periodically check for a node backed by the EC2 instance to join the cluster and for the node to have a status of 'ready.' Once a node becomes ready, NTH will complete the lifecycle hook, prompting the ASG to proceed with terminating the previous instance. If the lifecycle hook is not completed before the timeout, the ASG will take the default action. If the default action is 'ABANDON', the new instance will be terminated, and the notification process will be repeated with another new instance.

### Installation

#### Pod Security Admission

When using Kubernetes [Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/) it is recommended to assign the `[baseline](https://kubernetes.io/docs/concepts/security/pod-security-standards/#baseline)` level.

#### Helm

The easiest way to configure the various options of the termination handler is via [helm](https://helm.sh/). The chart for this project is hosted in [helm/aws-node-termination-handler](https://gallery.ecr.aws/aws-ec2/helm/aws-node-termination-handler)

To get started you need to authenticate your helm client

```
aws ecr-public get-login-password \
     --region us-east-1 | helm registry login \
     --username AWS \
     --password-stdin public.ecr.aws
```

Once that is complete you can install the termination handler. We've provided some sample setup options below. Make sure to replace CHART_VERSION with the version you want to install.

Minimal Config:

```sh
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/0123456789/my-term-queue \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

Webhook Configuration:

```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/0123456789/my-term-queue \
  --set webhookURL=https://hooks.slack.com/services/YOUR/SLACK/URL \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

Alternatively, pass Webhook URL as a Secret:

```
WEBHOOKURL_LITERAL="webhookurl=https://hooks.slack.com/services/YOUR/SLACK/URL"

kubectl create secret -n kube-system generic webhooksecret --from-literal=$WEBHOOKURL_LITERAL
```
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set enableSqsTerminationDraining=true \
  --set queueURL=https://sqs.us-east-1.amazonaws.com/0123456789/my-term-queue \
  --set webhookURLSecretName=webhooksecret \
  oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
```

For a full list of configuration options see our [Helm readme](https://github.com/aws/aws-node-termination-handler/blob/v1.25.0/config/helm/aws-node-termination-handler#readme).

#### Single Instance vs Multiple Replicas

The Helm chart, by default, will deploy a single instance of Amazon Node Termination Handler. With the minimizing of resource usage, a single instance still provides good responsiveness in processing SQS messages.

**When should multiple instances of Amazon Node Termination Handler be used?**

* Responsiveness: Amazon Node Termination Handler may be taking longer than desired to process certain events, potentially in processing numerous concurrent events or taking too long to drain Pods. The deployment of multiple Amazon Node Termination Handler instances may help.

* Availability: The deployment of multiple Amazon Node Termination Handler instances provides mitigation in the case that Amazon Node Termination Handler itself is drained. Replica Amazon Node Termination Handlers will process SQS messages, avoiding a delay until the Deployment can start another instance. 

**Notes**

* Running multiple instances of Amazon Node Termination Handler will not load balance responding to events. Each instance will greedily consume and respond to events.
* Logs from multiple instances of Amazon Node Termination Handler are not aggregated.
* Multiple instances of Amazon Node Termination Handler may respond to the same event, if it takes longer than 20s to process. This is not an error case, only the first response will have an affect.

#### Kubectl Apply

Queue Processor needs an **SQS queue URL** to function; therefore, manifest changes are **REQUIRED** before using kubectl to directly add all of the above resources into your cluster.

Minimal Config:

```
curl -L https://github.com/aws/aws-node-termination-handler/releases/download/v1.25.0/all-resources-queue-processor.yaml -o all-resources-queue-processor.yaml
<open all-resources-queue-processor.yaml and update QUEUE_URL value>
kubectl apply -f ./all-resources-queue-processor.yaml
```

For a full list of releases and associated artifacts see our [releases page](https://github.com/aws/aws-node-termination-handler/releases).

</details>


<details close>
<summary>Use with Kiam</summary>
<br>

## Use with Kiam

If you are using IMDS mode which defaults to `hostNetworking: true`, or if you are using queue-processor mode, then this section does not apply. The configuration below only needs to be used if you are explicitly changing NTH IMDS mode to `hostNetworking: false` .

To use the termination handler alongside [Kiam](https://github.com/uswitch/kiam) requires some extra configuration on Kiam's end.
By default Kiam will block all access to the metadata address, so you need to make sure it passes through the requests the termination handler relies on.

To add a whitelist configuration, use the following fields in the Kiam Helm chart values:

```
agent.whiteListRouteRegexp: '^\/latest\/meta-data\/(spot\/instance-action|events\/maintenance\/scheduled|instance-(id|type)|public-(hostname|ipv4)|local-(hostname|ipv4)|placement\/availability-zone)|\/latest\/dynamic\/instance-identity\/document$'
```
Or just pass it as an argument to the kiam agents:

```
kiam agent --whitelist-route-regexp='^\/latest\/meta-data\/(spot\/instance-action|events\/maintenance\/scheduled|instance-(id|type)|public-(hostname|ipv4)|local-(hostname|ipv4)|placement\/availability-zone)|\/latest\/dynamic\/instance-identity\/document$'
```

## Metadata endpoints
The termination handler relies on the following metadata endpoints to function properly:

```
/latest/dynamic/instance-identity/document
/latest/meta-data/spot/instance-action
/latest/meta-data/events/recommendations/rebalance
/latest/meta-data/events/maintenance/scheduled
/latest/meta-data/instance-id
/latest/meta-data/instance-life-cycle
/latest/meta-data/instance-type
/latest/meta-data/public-hostname
/latest/meta-data/public-ipv4
/latest/meta-data/local-hostname
/latest/meta-data/local-ipv4
/latest/meta-data/placement/availability-zone
```

</details>

## Building
For build instructions please consult [BUILD.md](./BUILD.md).

## Metrics
Available Prometheus metrics:

| Metric name    | Description                                                        |                              
| -------------- | -------------------------------------------------------------------|
| `actions`      | Number of actions                                                  |
| `actions_node` | Number of actions per node (Deprecated: Use actions metric instead)|
| `events_error` | Number of errors in events processing                              |

The method of collecting Prometheus metrics changes depending on whether NTH is running in IMDS mode or Queue mode.

> [!WARNING]
> Both `serviceMonitor` and `podMonitor` are custom resources provided by the [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) for seamless integration with Kubernetes services and pods. For more details, please refer to [the API reference docs](https://prometheus-operator.dev/docs/api-reference/api/) for the Prometheus Operator.

In Queue mode, metrics can be collected in two ways:
- Use a `serviceMonitor` custom resource with the Prometheus Operator to collect metrics.
- Alternatively, add aws-node-termination-handler service address statically in Prometheus `scrape_configs`.

Example `scrape_configs` in prometheus helm chart:
```yaml
# charts/prometheus/values.yaml
# See: https://github.com/prometheus-community/helm-charts/blob/main/charts/prometheus/values.yaml
extraScrapeConfigs: |
  - job_name: 'aws-node-termination-handler'
    static_configs:
      - targets:
          - 'aws-node-termination-handler.kube-system.svc.cluster.local:9092'
```

In IMDS mode, metrics can be collected as follows:
- Use a `podMonitor` custom resource with the Prometheus Operator to collect metrics.

## Communication
* If you've run into a bug or have a new feature request, please open an [issue](https://github.com/aws/aws-node-termination-handler/issues/new).
* You can also chat with us in the [Kubernetes Slack](https://kubernetes.slack.com) in the `#provider-aws` channel
* Check out the open source [Amazon EC2 Spot Instances Integrations Roadmap](https://github.com/aws/ec2-spot-instances-integrations-roadmap) to see what we're working on and give us feedback!

##  Contributing
Contributions are welcome! Please read our [guidelines](https://github.com/aws/aws-node-termination-handler/blob/main/CONTRIBUTING.md) and our [Code of Conduct](https://github.com/aws/aws-node-termination-handler/blob/main/CODE_OF_CONDUCT.md)

## License
This project is licensed under the Apache-2.0 License.