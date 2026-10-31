# Cluster tag isolation (Queue Processor mode)

When several clusters share an AWS account, each cluster's NTH needs access only to that cluster's queue, Auto Scaling groups and instances.
Cluster tag isolation does that with one tag, `eks:eks-cluster-name=<cluster name>`, on each of those resources:

- **IAM:** the tag-based policies in [`config/iam/`](../config/iam/) let NTH receive and delete messages only on a queue tagged for its cluster, and complete lifecycle actions only on Auto Scaling groups tagged for its cluster.
- **NTH:** with `--cluster-tag-isolation=enforce`, NTH also ignores events for instances that are not tagged for its cluster.

With EKS Pod Identity, one IAM role and policy serve every cluster in the account, because the cluster name comes from the Pod Identity session.
With IRSA, each cluster gets its own copy of a policy template with its name written in.

The IAM policy in earlier versions of the README needs no tags, but it grants NTH's actions on `"Resource": "*"`, so the NTH of each cluster has IAM permissions on every other cluster's queue and Auto Scaling groups.

- Setting up a new cluster: [New clusters](#new-clusters)
- Moving an existing cluster off the account-wide policy: [Existing clusters](#existing-clusters)
- Something isn't working: [What each mismatch does](#what-each-mismatch-does)

NTH never creates or tags AWS resources; an administrator applies the tags.
IMDS mode makes no AWS API calls, so none of this applies to it.

## IAM policies

IAM cannot use a policy variable in a condition key, so each policy hard-codes its tag key.
Set `--cluster-tag-key` to the key of the policy you attach:

| Policy | Credentials | Roles | Isolates clusters | `--cluster-tag-key` |
|---|---|---|---|---|
| [`config/iam/cluster-tag-pod-identity-policy.json`](../config/iam/cluster-tag-pod-identity-policy.json) | EKS Pod Identity | One role for every cluster | Yes | `eks:eks-cluster-name` |
| [`config/iam/cluster-tag-policy-template.json`](../config/iam/cluster-tag-policy-template.json) | IRSA | One role per cluster | Yes | `eks:eks-cluster-name` |
| An account-wide policy on `"Resource": "*"` (earlier versions of the README) | Any | Any | No | Any key, or unset |

Both tag-based policies have the same statements:

| Statement | Actions | Condition |
|---|---|---|
| `ConsumeOwnClusterQueue` | `sqs:ReceiveMessage`, `sqs:DeleteMessage` | Queue tag `eks:eks-cluster-name` equals the cluster name. The queue is in the caller's account. |
| `CompleteOwnClusterLifecycleActions` | `autoscaling:CompleteLifecycleAction`, `autoscaling:RecordLifecycleActionHeartbeat` | Auto Scaling group tag `eks:eks-cluster-name` equals the cluster name. |
| `DescribeWithoutResourceLevelPermissions` | `ec2:DescribeInstances`, `autoscaling:DescribeLifecycleHooks` | Pod Identity: region only (below). IRSA: none. |

Actions that cannot be scoped by tag, and what they can still reach:

| Action | Why | What it can reach |
|---|---|---|
| `ec2:DescribeInstances` | No resource-level permissions | Metadata and tags of every instance in the account. Read-only. |
| `autoscaling:DescribeLifecycleHooks` | No resource-level permissions | The lifecycle hook configuration of every Auto Scaling group in the account. Read-only. |

No mutating action is unscoped.

**Partition and account.** ARNs use `*` for the partition, region and account.
SQS queue policies can grant access to other accounts, so the queue statement also requires `"aws:ResourceAccount": "${aws:PrincipalAccount}"`: the queue must be in the same account as the NTH role.
Auto Scaling groups have no resource policies.

### EKS Pod Identity (recommended for new clusters)

[`cluster-tag-pod-identity-policy.json`](../config/iam/cluster-tag-pod-identity-policy.json) is plain JSON that you can attach as is.
The cluster name is `${aws:PrincipalTag/eks-cluster-name}`, the session tag that [EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identities.html) adds.
Use the standard [EKS Pod Identity trust policy](https://docs.aws.amazon.com/eks/latest/userguide/pod-id-role.html).

**One role for every cluster.** One IAM role with this policy can be shared by every cluster in the account and still isolate them: the trust policy names no cluster, and the session tags come from whichever cluster assumes the role.
Repeat only the pod identity association and the Helm values for each cluster.

**Region.** Every statement requires the requested region to be the region in the caller's `eks-cluster-arn` session tag (`"StringLike": {"aws:PrincipalTag/eks-cluster-arn": "arn:*:eks:${aws:RequestedRegion}:*:cluster/*"}`).
NTH can only use the role in its own cluster's region, so the queue must be in that region.

### IRSA

With [IAM roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) (IRSA), the session has no cluster tags, so the Pod Identity policy denies every call.
Write the cluster name into [`cluster-tag-policy-template.json`](../config/iam/cluster-tag-policy-template.json) instead, and attach it to an IRSA role used only by that cluster's NTH.
The file has the placeholder `${CLUSTER_NAME}`:

```json
{
  "Sid": "ConsumeOwnClusterQueue",
  "Effect": "Allow",
  "Action": ["sqs:ReceiveMessage", "sqs:DeleteMessage"],
  "Resource": "arn:*:sqs:*:*:*",
  "Condition": {
    "StringEquals": {
      "aws:ResourceTag/eks:eks-cluster-name": "${CLUSTER_NAME}",
      "aws:ResourceAccount": "${aws:PrincipalAccount}"
    }
  }
}
```

Replace it with `envsubst`, which leaves the IAM variables such as `${aws:PrincipalAccount}` alone:

```bash
CLUSTER_NAME=my-cluster envsubst '${CLUSTER_NAME}' < config/iam/cluster-tag-policy-template.json > nth-policy.json
```

The cluster name must match `--cluster-name` and the tag value on the queue, the Auto Scaling groups and the instances.

**One role per cluster.** Each cluster's role names its cluster, so create a role and a policy for each cluster.

**Region.** The policy template has no region condition.
If clusters in different regions of one account have the same name, add `"aws:RequestedRegion": "<region>"` to the `StringEquals` of each statement.

The same policy works with other credentials that are used only by one cluster's NTH.

### Account-wide policies

A policy on `"Resource": "*"` does not isolate clusters: the NTH of any cluster using it has IAM permissions to receive and delete messages on every queue, and to complete lifecycle actions on every Auto Scaling group, in the account.
If your NTH uses one today, move to a tag-based policy with the steps in [Existing clusters](#existing-clusters). `enforce` works with an account-wide policy: NTH ignores events for instances of other clusters, but IAM does not limit it to its own queue and Auto Scaling groups.

The policy in earlier versions of the README omits `autoscaling:RecordLifecycleActionHeartbeat` and `autoscaling:DescribeLifecycleHooks`, which NTH calls when `--heartbeat-interval` is set.
The tag-based policies include both.

### Isolation covers the whole account

A cluster's queue and Auto Scaling groups are protected only when every NTH role in the account uses a tag-based policy.
A cluster whose NTH uses an account-wide policy can receive and delete messages on any queue, and complete lifecycle actions on any Auto Scaling group, in the account.

Use one queue per cluster.
IAM can scope NTH by queue, but not by the instance an individual message refers to.

## New clusters

> [!IMPORTANT]
> These steps are for new clusters.
> In an existing cluster, tag the queue, the Auto Scaling groups and the running instances first; see [Existing clusters](#existing-clusters).

Replace `my-cluster`, `111122223333` and the names below with your own.

```bash
CLUSTER=my-cluster
TAG_KEY=eks:eks-cluster-name
```

1. Create the queue with the cluster tag (see the [README](../README.md#1-create-an-sqs-queue) for the queue policy):

   ```bash
   aws sqs create-queue --queue-name "$CLUSTER-nth" \
     --tags "$TAG_KEY=$CLUSTER" --attributes file:///tmp/queue-attributes.json
   ```

2. Tag each Auto Scaling group, and propagate the tag to its instances:

   ```bash
   aws autoscaling create-or-update-tags --tags \
     "ResourceId=my-asg,ResourceType=auto-scaling-group,Key=$TAG_KEY,Value=$CLUSTER,PropagateAtLaunch=true"
   ```

3. Create the role.

   With EKS Pod Identity: install the EKS Pod Identity Agent, create a role with the [EKS Pod Identity trust policy](https://docs.aws.amazon.com/eks/latest/userguide/pod-id-role.html), attach the policy, and associate the role with the NTH service account.
   The agent must be installed as an EKS add-on; NTH itself is installed with Helm or the manifests as usual.
   The role can be shared by every cluster in the account; repeat only the association for each cluster.

   ```bash
   aws eks create-addon --cluster-name "$CLUSTER" --addon-name eks-pod-identity-agent
   curl -fsSLO https://raw.githubusercontent.com/aws/aws-node-termination-handler/main/config/iam/cluster-tag-pod-identity-policy.json
   aws iam create-role --role-name nth-cluster-tag \
     --assume-role-policy-document file://pod-identity-trust-policy.json
   aws iam put-role-policy --role-name nth-cluster-tag --policy-name cluster-tag-policy \
     --policy-document file://cluster-tag-pod-identity-policy.json
   aws eks create-pod-identity-association --cluster-name "$CLUSTER" --namespace kube-system \
     --service-account aws-node-termination-handler --role-arn arn:aws:iam::111122223333:role/nth-cluster-tag
   ```

   With IRSA: create an [IRSA role](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) for the `kube-system/aws-node-termination-handler` service account, and attach the policy template with the cluster name:

   ```bash
   curl -fsSLO https://raw.githubusercontent.com/aws/aws-node-termination-handler/main/config/iam/cluster-tag-policy-template.json
   CLUSTER_NAME="$CLUSTER" envsubst '${CLUSTER_NAME}' < cluster-tag-policy-template.json > nth-policy.json
   aws iam put-role-policy --role-name "$CLUSTER-nth" --policy-name cluster-tag-policy \
     --policy-document file://nth-policy.json
   ```

4. Install NTH with the cluster tag settings.
   With IRSA, also add `--set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::111122223333:role/my-cluster-nth`.

   ```bash
   helm upgrade --install aws-node-termination-handler \
     --namespace kube-system \
     --set enableSqsTerminationDraining=true \
     --set queueURL=https://sqs.us-west-2.amazonaws.com/111122223333/my-cluster-nth \
     --set clusterName=my-cluster \
     --set clusterTagKey=eks:eks-cluster-name \
     --set clusterTagIsolation=enforce \
     oci://public.ecr.aws/aws-ec2/helm/aws-node-termination-handler --version $CHART_VERSION
   ```

   The same settings as flags: `--cluster-name=my-cluster --cluster-tag-key=eks:eks-cluster-name --cluster-tag-isolation=enforce`.
   As environment variables: `CLUSTER_NAME`, `CLUSTER_TAG_KEY` and `CLUSTER_TAG_ISOLATION`.

At startup NTH logs one line with its settings, in every mode.
With `jsonLogging: true`:

```json
{"level":"info","clusterName":"my-cluster","tagKey":"eks:eks-cluster-name","clusterTagIsolation":"enforce","time":"2026-10-21T21:49:05Z","message":"cluster tag isolation settings"}
```

The value of `clusterTagIsolation` must be exactly `disable` or `enforce`; an empty value means `disable`.
NTH rejects any other value, such as `Enforce` or `false`, with a message that names `--cluster-tag-isolation`.

When an instance of this cluster is terminated, NTH completes its lifecycle hook as before:

```json
{"level":"info","lifecycleHookName":"my-cluster-term","instanceID":"i-0ce25558b9d68136c","time":"2026-10-21T21:51:05Z","message":"Completed ASG Lifecycle Hook"}
```

## Existing clusters

`--cluster-tag-isolation` has two states.
Set it to match the IAM policy you attached:

| State | IAM attached | NTH behavior |
|---|---|---|
| `disable` (default) | Any policy | Events are handled without checking the cluster tag. |
| `enforce` | A tag-based policy (and, during the rollout, the account-wide policy) | Requires `--cluster-name` and `--cluster-tag-key`. Ignores queue events for instances whose cluster tag is missing or is not this cluster, and deletes those messages from the queue. |

Roll out in this order.
Every step keeps NTH handling this cluster's events:

1. **Tag the resources.** Tag the queue, every Auto Scaling group (with `PropagateAtLaunch=true`), and every running instance in those groups with `eks:eks-cluster-name=<cluster name>`. `PropagateAtLaunch` only affects new instances, so tag the running ones yourself:

   ```bash
   aws sqs tag-queue --queue-url "$QUEUE_URL" --tags "$TAG_KEY=$CLUSTER"
   aws autoscaling create-or-update-tags --tags \
     "ResourceId=my-asg,ResourceType=auto-scaling-group,Key=$TAG_KEY,Value=$CLUSTER,PropagateAtLaunch=true"
   aws ec2 create-tags --resources i-0123456789abcdef0 i-0fedcba9876543210 --tags "Key=$TAG_KEY,Value=$CLUSTER"
   ```

2. **Confirm the tags** with the commands in [How to confirm the resources are tagged](#how-to-confirm-the-resources-are-tagged).
3. **Attach a tag-based policy alongside the account-wide policy.**
   - IRSA: attach the policy template with this cluster's name to the role NTH already uses.
     Nothing else changes.
   - EKS Pod Identity: move NTH to Pod Identity (step 3 of [New clusters](#new-clusters)) with a role that has both the Pod Identity policy and the account-wide policy attached.
     Remove the `eks.amazonaws.com/role-arn` annotation from the NTH service account if it has one: the AWS SDK uses IRSA credentials ahead of Pod Identity credentials.

   Set `clusterName` and `clusterTagKey`, keep `clusterTagIsolation: disable`, and restart NTH.
4. **Set `clusterTagIsolation: enforce`.** Watch the logs of every replica for `ignoring resource not tagged for this cluster` (see [below](#how-to-confirm-the-resources-are-tagged)), and tag every instance of this cluster it reports in `resource`.
5. **Detach the account-wide policy** from the role.
   Isolation starts here.
   Remove the NTH permissions from any role NTH used before, including the node role.

If you later change `--cluster-tag-key`, for example to use your own policy, tag the resources with the new key and roll out again.

## What each mismatch does

NTH cannot check which IAM policy is attached.
This is the result of each mismatch:

| Setting | Policy attached | Result |
|---|---|---|
| `enforce` | Account-wide policy only | Everything works, but IAM isolates nothing. NTH ignores other clusters' instances, but IAM allows it to use other clusters' queues and Auto Scaling groups. |
| Any | A tag-based policy alongside the account-wide policy | Everything works, but IAM isolates nothing: the account-wide policy allows every call. Detach it (rollout step 5). |
| Any | Pod Identity policy, credentials without the Pod Identity session tags (IRSA, the node role, static keys) | Every call is denied. NTH keeps running but handles no events, and logs `There was a problem monitoring for events` with the `AccessDenied` error on every poll. Use the policy template for IRSA. |
| Any | Policy template with a cluster name that is not the queue's tag value, or with `${CLUSTER_NAME}` not replaced | Same as an untagged queue (next row). |
| Any | Tag-based policy, queue not tagged | Every `ReceiveMessage` fails with `AccessDenied`: NTH handles no events, and logs `There was a problem monitoring for events` on every poll until the queue is tagged. IAM uses the cluster name in the policy or session tag, not the NTH setting. |
| Any | Tag-based policy, an Auto Scaling group not tagged | NTH drains the node, then `CompleteLifecycleAction` fails with `AccessDenied` and NTH logs `There was a problem executing the post-drain task`. The message stays in the queue, and the instance waits for the lifecycle hook timeout. |
| `enforce`, `clusterTagKey` not `eks:eks-cluster-name` | Tag-based policy | IAM checks `eks:eks-cluster-name` on the queue and Auto Scaling groups, and NTH checks the other key on the instances. Both tags are needed. Set `clusterTagKey` to the key of the policy. |
| `enforce`, `clusterName` not the cluster name in IAM | Tag-based policy | IAM allows the queue and Auto Scaling groups tagged with the EKS cluster name (Pod Identity) or the name in the policy (IRSA), but NTH drops every event for instances tagged with it: the nodes are not drained. Set `clusterName` to the same name. |
| `enforce` | Any, instances not tagged yet | Events for those instances are dropped and deleted from the queue, with an info log `ignoring resource not tagged for this cluster`. The nodes are not drained, and Auto Scaling continues after the lifecycle hook timeout. Tag the instances before setting `enforce`. |
| `enforce` | Any, IMDS mode | NTH fails at startup with `currently using IMDS mode. cluster-tag-isolation=enforce is only supported for Queue Processor mode`. The Helm chart fails to render. |
| Any other value | Any | NTH fails at startup with `invalid --cluster-tag-isolation "<value>": must be "disable" or "enforce"`. The Helm chart fails to render. |
| `clusterTagKey` starts with `aws:` | Any | NTH fails at startup with `invalid cluster-tag-key "<key>": the aws: prefix is reserved for tags that AWS applies`. The Helm chart fails to render. |

An instance of this cluster that is tagged for another cluster has all its events dropped.
Decide which cluster owns it, instead of re-tagging it to make the log line go away.

## How to confirm the resources are tagged

NTH does not tag resources, so it never reports completion.
Check the three resource types yourself.
Each command prints only the resources still missing the cluster tag, with the value they have now (`None` when they have no cluster tag), and prints nothing when everything is tagged.

```bash
CLUSTER=my-cluster
TAG_KEY=eks:eks-cluster-name
QUEUE_URL=https://sqs.us-west-2.amazonaws.com/111122223333/my-cluster-nth
```

Queue:

```bash
aws sqs list-queue-tags --queue-url "$QUEUE_URL" \
  --query "[@][?!(Tags.\"$TAG_KEY\" == '$CLUSTER')].['$QUEUE_URL', Tags.\"$TAG_KEY\"]" --output text
```

Auto Scaling groups, listing those without a propagated cluster tag:

```bash
aws autoscaling describe-auto-scaling-groups --auto-scaling-group-names my-asg my-other-asg \
  --query "AutoScalingGroups[?!(Tags[?Key=='$TAG_KEY' && Value=='$CLUSTER' && PropagateAtLaunch])].[AutoScalingGroupName, Tags[?Key=='$TAG_KEY']|[0].Value]" \
  --output text
```

Instances of this cluster's nodes, one call per instance (one missing instance would fail a batched call):

```bash
kubectl get nodes -o jsonpath='{range .items[*]}{.spec.providerID}{"\n"}{end}' | awk -F/ '{print $NF}' |
  xargs -I{} aws ec2 describe-instances --instance-ids {} \
    --query "Reservations[].Instances[] | [?!(Tags[?Key=='$TAG_KEY' && Value=='$CLUSTER'])].[InstanceId, Tags[?Key=='$TAG_KEY']|[0].Value]" \
    --output text
```

After `enforce` is set, NTH logs each dropped event at info level as `ignoring resource not tagged for this cluster`, with `resource` (the instance ID), `tagKey`, `clusterName` and `tagValue` (the value found, empty when the instance has no cluster tag).
Every replica handles events on its own, so read the logs of all of them:

```bash
kubectl -n kube-system logs -l app.kubernetes.io/name=aws-node-termination-handler -c aws-node-termination-handler \
  --prefix --tail=-1 | grep 'ignoring resource not tagged for this cluster'
```

For example, with `jsonLogging: true`, an instance of another cluster and then an instance that still needs the cluster tag:

```json
{"level":"info","resource":"i-0f01d8f12c456b360","tagKey":"eks:eks-cluster-name","clusterName":"my-cluster","tagValue":"other-cluster","time":"2026-10-21T21:50:00Z","message":"ignoring resource not tagged for this cluster"}
{"level":"info","resource":"i-08de9f30f2e78e1b9","tagKey":"eks:eks-cluster-name","clusterName":"my-cluster","tagValue":"","time":"2026-10-21T21:50:02Z","message":"ignoring resource not tagged for this cluster"}
```

Tagging is complete when the three commands above print nothing and no `ignoring resource not tagged for this cluster` line with an empty `tagValue` appears for this cluster's instances.

## Settings

| Flag | Environment variable | Helm value | Default | Description |
|---|---|---|---|---|
| `--cluster-name` | `CLUSTER_NAME` | `clusterName` | `""` | The name of the Kubernetes cluster NTH runs in: the EKS cluster name with the Pod Identity policy, the name written into the policy template. Queue Processor mode only. |
| `--cluster-tag-key` | `CLUSTER_TAG_KEY` | `clusterTagKey` | `""` | The tag key whose value is the cluster name on this cluster's instances, Auto Scaling groups and queue. NTH never writes it. The tag-based policies use `eks:eks-cluster-name`. Must not start with `aws:`. Queue Processor mode only. |
| `--cluster-tag-isolation` | `CLUSTER_TAG_ISOLATION` | `clusterTagIsolation` | `disable` | `disable` or `enforce`, set to match the attached IAM policy (see [Existing clusters](#existing-clusters)). `enforce` requires `--cluster-name` and `--cluster-tag-key`, and fails at startup in IMDS mode. |

In `disable`, setting only one of `--cluster-name` and `--cluster-tag-key` logs the warning `only one of cluster-name and cluster-tag-key is set; cluster-tag-isolation=enforce requires both`.

With `enforce` and `--enable-prometheus-server`, the `nth_tagged_instances` and `nth_tagged_nodes` metrics count only instances tagged for this cluster.

The existing `--managed-tag` check (`--check-tag-before-draining`) is unchanged and applies in addition to the cluster tag.
