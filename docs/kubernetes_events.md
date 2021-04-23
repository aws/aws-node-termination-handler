# AWS Node Termination Handler Kubernetes events

AWS Node Termination Handler has the ability to emit a Kubernetes event every time an interruption signal is sent from AWS and also every time an operation is attempted on a node. More information on how to get events can be found [here](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-application-introspection/).

## Configuration

There are two relevant parameters:

* `emit-kubernetes-events`

	If true, Kubernetes events will be emitted when interruption events are received and when actions are taken on Kubernetes nodes. Defaults to `false`

*  `kubernetes-events-extra-annotations`

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

## Default annotations

If `emit-kubernetes-events` is enabled, AWS Node Termination Handler will automatically inject a set of annotations to each event it emits. Such annotations are gathered from the underlying host's IMDS endpoint and enrich each event with information about the host that emitted it.

The default annotations are:

Name | Example value
--- | ---
`account-id` | `123456789012` 
`availability-zone` | `us-west-2a`
`instance-id` | `i-abcdef12345678901`
`instance-type` | `m5.8xlarge`
`local-hostname` | `ip-10-1-2-3.us-west-2.compute.internal`
`local-ipv4` | `10.1.2.3`
`public-hostname` | `my-example.host.net`
`public-ipv4` | `42.42.42.42`
`region` | `us-west-2`

If `kubernetes-events-extra-annotations` are specified they will be appended to the above. In case of collision, the user-defined annotation wins.

## How to get events

All events belong to Kubernetes `Node` objects so they belong in the `default` namespace. The event source is `aws-node-termination-handler`. From command line, use `kubectl` to get the events as follows:

```sh
kubectl get events --field-selector "source=aws-node-termination-handler"
```

To narrow down the search you can use multiple field selectors, like:

```sh
kubectl get events --field-selector "reason=SpotInterruption,involvedObject.name=ip-10-1-2-3.us-west-2.compute.internal"
```

Results can also be printed out in JSON or YAML format and piped to processors like `jq` or `yq`. Then, the above annotations can also be used for discovery and filtering.

## Caveats

### Default annotations in Queue Processor Mode

Default annotations values are gathered from the IMDS endpoint local to the Node on which AWS Node Termination Handler runs. This is fine when running on IMDS Processor Mode since an AWS Node Termination Handler Pod will be deployed to all Nodes via a `DaemonSet` and each Node will emit all events related to itself with its own default annotations.

However, when running in Queue Processor Mode AWS Node Termination Handler is deployed to a number of Nodes (1 replica by default) since it's done via a `Deployment`. In that case the default annotations values will be gathered from the Node(s) running AWS Node Termination Handler, and so the values in the default annotations stamped to all events will match those of the Node from which the event was emitted, not those of the Node of which the event is about.
