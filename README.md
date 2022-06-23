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

## Getting Started

### Infrastructure Setup

TBD

### Installation and Configuration

For a full list of inputs see the Helm chart [README.md](./charts/aws-node-termination-handler-2/README.md).

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
