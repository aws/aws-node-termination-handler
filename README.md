<h1>AWS Node Termination Handler</h1>

<h4>Gracefully handle EC2 instance shutdown within Kubernetes</h4>

## Project Summary

This project ensures that the Kubernetes control plane responds appropriately to events that can cause your EC2 instance to become unavailable, such as [EC2 maintenance events](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html), [EC2 Spot interruptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/spot-interruptions.html), [ASG Scale-In](https://docs.aws.amazon.com/autoscaling/ec2/userguide/AutoScalingGroupLifecycle.html#as-lifecycle-scale-in), [ASG AZ Rebalance](https://docs.aws.amazon.com/autoscaling/ec2/userguide/auto-scaling-benefits.html#AutoScalingBehavior.InstanceUsage), and EC2 Instance Termination via the API or Console.  If not handled, your application code may not stop gracefully, take longer to recover full availability, or accidentally schedule work to nodes that are going down.

## Major Features

- Monitors an SQS Queue for:
  - EC2 Spot Interruption Notifications
  - EC2 Instance Rebalance Recommendation
  - EC2 Auto-Scaling Group Termination Lifecycle Hooks to take care of ASG Scale-In, AZ-Rebalance, Unhealthy Instances, and more!
  - EC2 Status Change Events
  - EC2 Scheduled Change events from AWS Health
- Webhook feature to send shutdown or restart notification messages
- Unit & Integration Tests

## Differences from v1

The first major version of AWS Node Termination Handler (NTH) originally operated as a daemonset deployed to every desired node in the cluster (aka IMDS Mode); later, we added the option to deploy a single pod which read events for the entire cluster from an SQS queue (aka Queue Processor Mode). Both heavily utilized Helm for configuration, and changing configuration meant updating the deployment.

This second major version of NTH aims to refine the Queue Processor Mode. Only a single pod is deployed and configuration is done using a new custom resource called *Terminators*. A *Terminator* contains much of the configuration about where NTH should fetch events, what actions to take for a given event type, filter nodes to act upon, and webhook notifications. Multiple *Terminators* may be deployed, modified, or removed without needing to redeploy NTH itself.

## Getting Started

### 1. Setup Infrastructure

#### 1.1. Create an IAM OIDC Provider

Your EKS cluster must have an IAM OIDC Provider. Follow the steps in [Create an IAM OIDC provider for your cluster](https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html) to determine whether your EKS cluster already has an IAM OIDC Provider and, if necessary, create one.

#### 1.2. Create the NTH Service Account

##### 1.2.1. Create the IAM Policy

Download the service account policy template for AWS CloudFormation at https://github.com/aws/aws-node-termination-handler/releases/download/v1.14.1/infrastructure.yaml

Then create the IAM Policy by deploying the AWS CloudFormation stack:
```sh
aws cloudformation deploy \
  --template-file infrastructure.yaml \
  --stack-name nth-service-account \
  --capabilities CAPABILITY_NAMED_IAM
```

##### 1.2.2. Create the Service Account

Use either the AWS CLI or AWS Console to lookup the ARN of the IAM Policy for the service account.

Create the cluster service account using the following command:
```sh
eksctl create iamserviceaccount \
  --cluster <CLUSTER NAME> \
  --namespace <NAMESPACE> \
  --name "nth-service-account" \
  --role-name "nth-service-account" \
  --attach-policy-arn <SERVICE ACCOUNT POLICY ARN> \
  --role-only \
  --approve
```

### 2. Deploy NTH

Get the ARN of the service account role:
```sh
eksctl get iamserviceaccount \
  --cluster <CLUSTER NAME> \
  --namespace <NAMESPACE> \
  --name "nth-service-account"
```

Add the AWS `eks-charts` helm repository and deploy the chart:
```sh
helm repo add eks https://aws.github.io/eks-charts

helm upgrade \
  --install \
  nth \
  eks/aws-node-termination-handler-2 \
  --namespace <NAMESPACE> \
  --create-namespace \
  --set aws.region=<AWS REGION> \
  --set serviceAccount.name="nth-service-account" \
  --set serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn=<SERVICE ACCOUNT ROLE ARN>
```

For a full list of inputs see the Helm chart `README.md`.

### 3. Create a Terminator

#### 3.1. Create an SQS Queue

NTH reads events from one or more SQS Queues. If you already have an SQS Queue available then you may skip this step.

*Note:* Multiple Terminators may read from a single SQS Queue. A Terminator will only delete a message if a matching node was found in the cluster. The SQS Queue's visibility window setting can help to ensure that a message is delivered to only one Terminator at a time.

You may create your own SQS Queue but an AWS CloudFormation template is available that will create an SQS Queue and commonly used rules for AWS EventBridge. Download from https://github.com/aws/aws-node-termination-handler/releases/download/v1.14.1/queue-infrastructure.yaml

```sh
aws cloudformation deploy \
  --template-file queue-infrastructure.yaml \
  --stack-name nth-queue \
  --parameter-overrides \
      ClusterName=<CLUSTER NAME> \
      QueueName=<QUEUE NAME>
```

#### 3.2. Define and deploy a Terminator

You may download a template file from https://github.com/aws/aws-node-termination-handler/releases/download/v1.14.1/terminator.yaml.tmpl. Edit the file with the required fields and desired configuration.

Deploy the Terminator:
```sh
kubectl apply -f <FILENAME>
```

## Metrics

TBD

## Communication
* If you've run into a bug or have a new feature request, please open an [issue](https://github.com/aws/aws-node-termination-handler/issues/new).
* You can also chat with us in the [Kubernetes Slack](https://kubernetes.slack.com) in the `#provider-aws` channel
* Check out the open source [Amazon EC2 Spot Instances Integrations Roadmap](https://github.com/aws/ec2-spot-instances-integrations-roadmap) to see what we're working on and give us feedback!

##  Contributing
Contributions are welcome! Please read our [guidelines](https://github.com/aws/aws-node-termination-handler/blob/main/CONTRIBUTING.md) and our [Code of Conduct](https://github.com/aws/aws-node-termination-handler/blob/main/CODE_OF_CONDUCT.md)

To setup a development environment see the instructions in [DEVELOPMENT.md](./DEVELOPMENT.md).

## License
This project is licensed under the Apache-2.0 License.
