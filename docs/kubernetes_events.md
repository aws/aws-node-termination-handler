# AWS Node Termination Handler Kubernetes events

AWS Node Termination Handler has the ability to emit a Kubernetes event every time an interruption signal is sent from AWS and also every time an operation is attempted on a node. More information on how to get events can be found [here](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-application-introspection/).

## Configuration

There are two relevant parameters:

* `emit-kubernetes-events`:

	If true, Kubernetes events will be emitted when interruption events are received and when actions are taken on Kubernetes nodes. Defaults to `false`.

*  `kubernetes-events-extra-annotations`:

	A comma-separated list of `key=value` extra annotations to attach to all emitted Kubernetes events. Example:
	
	`"first=annotation,sample.annotation/number=two"`

## Event reasons

There are a number of events that can be emitted, each one with a reason that can be used to quickly identify the event nature and for filtering. Each event will also have a message with extended information. Here's a reasons summary:

AWS interruption event reasons:

* `RebalanceRecommendation`
* `ScheduledEvent`
* `SQSTermination`
* `SpotInterruption`

Node action reasons:

* `Cordon`
* `CordonError`
* `CordonAndDrain`
* `CordonAndDrainError`
* `PreDrain`
* `PreDrainError`
* `PostDrain`
* `PostDrainError`
* `Uncordon`
* `UncordonError`
* `MonitorError`

## Default IMDS mode annotations

If `emit-kubernetes-events` is enabled and `enable-sqs-termination-draining` is disabled (meaning we're operating in IMDS mode), AWS Node Termination Handler will automatically inject a set of annotations to each event it emits. Such annotations are gathered from the underlying host's IMDS endpoint and enrich each event with information about the host that emitted it.

_**NOTE**: In Queue Processor mode, the default IMDS mode annotations will be disabled but you can still define a set of extra annotations._

The default IMDS mode annotations are:

Name | Example value
--- | ---
`account-id` | `123456789012` 
`availability-zone` | `us-west-2a`
`instance-id` | `i-abcdef12345678901`
`instance-life-cycle` | `spot`
`instance-type` | `m5.8xlarge`
`local-hostname` | `ip-10-1-2-3.us-west-2.compute.internal`
`local-ipv4` | `10.1.2.3`
`public-hostname` | `my-example.host.net`
`public-ipv4` | `42.42.42.42`
`region` | `us-west-2`

If `kubernetes-events-extra-annotations` are specified they will be appended to the above. In case of collision, the user-defined annotation wins.

## How to get events

All events are about Kubernetes `Node` objects so they belong in the `default` namespace. The event source is `aws-node-termination-handler`. From command line, use `kubectl` to get the events as follows:

```sh
kubectl get events --field-selector "source=aws-node-termination-handler"
```

To narrow down the search you can use multiple field selectors, like:

```sh
kubectl get events --field-selector "reason=SpotInterruption,involvedObject.name=ip-10-1-2-3.us-west-2.compute.internal"
```

Results can also be printed out in JSON or YAML format and piped to processors like `jq` or `yq`. Then, the above annotations can also be used for discovery and filtering.
