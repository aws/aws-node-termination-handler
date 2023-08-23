# Setup Node Termination Handler with Terraform Providers

## Helm chart provider example

```hcl
resource "helm_release" "node_termination_handler" {
  name      = "aws-node-termination-handler"
  namespace = "kube-system"

  chart      = "aws-node-termination-handler"
  repository = "https://aws.github.io/eks-charts/"
  version    = "0.21.0"

  set {
    name  = "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn"
    value = aws_iam_role.aws_node_termination_handler_role.arn
  }

  set {
    name  = "awsRegion"
    value = var.aws_region
  }

  set {
    name  = "queueURL"
    value = aws_sqs_queue.main.url
  }

  set {
    name  = "checkTagBeforeDraining"
    value = false
  }

  set {
    name  = "enableSqsTerminationDraining"
    value = true
  }
}
```

## Apply Terraform  

```bash
terraform init 
```

```bash
terraform apply --auto-approve 
```