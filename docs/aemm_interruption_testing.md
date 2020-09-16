# AWS Node Termination Handler & Amazon EC2 Metadata Mock

We have open sourced a tool called the [amazon-ec2-metadata-mock](https://github.com/aws/amazon-ec2-metadata-mock) (AEMM)
that simulates spot interruption notices and more by starting a real webserver that serves data similar to EC2 Instance
Metadata Service. The tool is easily deployed to kubernetes with a Helm chart.

Below is a short guide on how to set AEMM up with your Node Termination Handler cluster in case you'd like to verify the
behavior yourself.

## Triggering AWS Node Termination Handler with Amazon EC2 Metadata Mock

Start by installing AEMM on your cluster. For full and up to date installation instructions reference the AEMM repository.
Here's just one way to do it.

Download the latest tar ball from the releases page, at the time of writing this that was v1.6.0. Then install it using
Helm:
```
helm install amazon-ec2-metadata-mock amazon-ec2-metadata-mock-1.6.0.tgz \
  --namespace default
```

Once AEMM is installed, you need to change the instance metadata url of Node Termination Handler to point
to the location AEMM is serving from. If you use the default values of AEMM, the installation will look similar to this:
```
helm upgrade --install aws-node-termination-handler \
  --namespace kube-system \
  --set instanceMetadataURL="http://amazon-ec2-metadata-mock-service.default.svc.cluster.local:1338" \
  eks/aws-node-termination-handler
```

That's it! Instead of polling the real IMDS service endpoint, AWS Node Termination Handler will instead poll AEMM.
If you open the logs of an AWS Node Termination Handler pod you should see that it receives (mock) interruption
events from AEMM and that the nodes are cordoned and drained. Keep in mind that these nodes won't actually get terminated,
so you might need to manually uncordon the nodes if you want to reset your test cluster.

### AEMM Advanced Configuration
If you run the example above you might notice that the logs are heavily populated. Here's an example output:
```
2020/09/15 21:13:41 Sending interruption event to the interruption channel
2020/09/15 21:13:41 Got interruption event from channel {InstanceID:i-1234567890abcdef0 InstanceType:m4.xlarge PublicHostname:ec2-192-0-2-54.compute-1.amazonaws.com PublicIP:192.0.2.54 LocalHostname:ip-172-16-34-43.ec2.internal LocalIP:172.16.34.43 AvailabilityZone:us-east-1a} {EventID:spot-itn-47ddfb5e39791606bec3e91fea4cdfa86f86a60ddaf014c8b4af8e008f134b19 Kind:SPOT_ITN Description:Spot ITN received. Instance will be interrupted at 2020-09-15T21:15:41Z
 State: NodeName:ip-192-168-123-456.us-east-1.compute.internal StartTime:2020-09-15 21:15:41 +0000 UTC EndTime:0001-01-01 00:00:00 +0000 UTC Drained:false PreDrainTask:0x113c8a0 PostDrainTask:<nil>}
WARNING: ignoring DaemonSet-managed Pods: default/amazon-ec2-metadata-mock-pszj2, kube-system/aws-node-bl2bj, kube-system/aws-node-termination-handler-2pvjr, kube-system/kube-proxy-fct9f
evicting pod "coredns-67bfd975c5-rgkh7"
evicting pod "coredns-67bfd975c5-6g88n"
2020/09/15 21:13:42 Node "ip-192-168-123-456.us-east-1.compute.internal" successfully cordoned and drained.
2020/09/15 21:13:43 Sending interruption event to the interruption channel
2020/09/15 21:13:43 Got interruption event from channel {InstanceID:i-1234567890abcdef0 InstanceType:m4.xlarge PublicHostname:ec2-192-0-2-54.compute-1.amazonaws.com PublicIP:192.0.2.54 LocalHostname:ip-172-16-34-43.ec2.internal LocalIP:172.16.34.43 AvailabilityZone:us-east-1a} {EventID:spot-itn-97be476b6246aba6401ba36e54437719bfdf987773e9c83fe30336eb7fea9704 Kind:SPOT_ITN Description:Spot ITN received. Instance will be interrupted at 2020-09-15T21:15:43Z
 State: NodeName:ip-192-168-123-456.us-east-1.compute.internal StartTime:2020-09-15 21:15:43 +0000 UTC EndTime:0001-01-01 00:00:00 +0000 UTC Drained:false PreDrainTask:0x113c8a0 PostDrainTask:<nil>}
WARNING: ignoring DaemonSet-managed Pods: default/amazon-ec2-metadata-mock-pszj2, kube-system/aws-node-bl2bj, kube-system/aws-node-termination-handler-2pvjr, kube-system/kube-proxy-fct9f
2020/09/15 21:13:44 Node "ip-192-168-123-456.us-east-1.compute.internal" successfully cordoned and drained.
2020/09/15 21:13:45 Sending interruption event to the interruption channel
2020/09/15 21:13:45 Got interruption event from channel...
```

This isn't a mistake, by default AEMM will respond to any request for metadata with a spot interruption occurring 2 minutes
later than the request time.\* AWS Node Termination Handler polls for events every 2 seconds by default, so the effect is
that new interruption events are found and processed every 2 seconds. 

In reality there will only be a single interruption event, and you can mock this by setting the `spot.time` parameter of
AEMM when installing it. 
```
helm install amazon-ec2-metadata-mock amazon-ec2-metadata-mock-1.6.0.tgz \
  --set aemm.spot.time="2020-09-09T22:40:47Z" \
  --namespace default
```

Now when you check the logs you should only see a single event get processed. 

For more ways of configuring AEMM check out the [Helm configuration page](https://github.com/aws/amazon-ec2-metadata-mock/tree/master/helm/amazon-ec2-metadata-mock).

## Node Termination Handler E2E Tests

AEMM started out as a test server for aws-node-termination-handler's end-to-end tests in this repo. We use AEMM throughout
our end to end tests to create interruption notices.

The e2e tests install aws-node-termination-handler using Helm and set the metadata url [here](https://github.com/aws/aws-node-termination-handler/blob/master/test/e2e/spot-interruption-test#L36).
This becomes where aws-node-termination-handler looks for metadata; other applications on the node still look at the real
EC2 metadata service.

We set the metadata url environment variable [here](https://github.com/aws/aws-node-termination-handler/blob/master/test/k8s-local-cluster-test/run-test#L18)
for the local tests that use a kind cluster, and [here](https://github.com/aws/aws-node-termination-handler/blob/master/test/eks-cluster-test/run-test#L117)
for the eks-cluster e2e tests.

Check out the [ReadMe](https://github.com/aws/aws-node-termination-handler/tree/master/test) in our test folder for more
info on the e2e tests. 

---

\* Only the first two unique IPs to request data from AEMM receive spot itn information in the response. This was introduced
in AEMM v1.6.0. This can be overridden with a configuration parameter. For previous versions there is no unique IP restriction.
