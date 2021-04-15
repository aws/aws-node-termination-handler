## How do NTH end-to-end tests work?


#### TL;DR
Instead of expected/actual *values*, the tests evaluate expected/actual **states**. NTH's functionality is evaluated by triggering various interruption events on the cluster (spot interrupts, maintenance events) and ensuring the nodes and pods in the cluster are in their proper state (cordoned, tainted, evicted, etc.). The tests fail if the nodes/pods don't transition into expected states suggesting an issue in NTH logic or cluster setup.


#### Summary
This doc details how the end-to-end (e2e) tests work for aws-node-termination-handler (NTH) at a high-level. These tests are no different from normal integration tests in that they capture the functionality of NTH. However, the *assert-actual-equals-expected* pattern is not as explicit as in other e2e tests which can cause some confusions. We hope to bring clarity with the content below.


#### Starting Tests
**Make Targets**

The e2e tests can be run on a local cluster or an eks cluster using one of the following `make` targets:
 * `make e2e-test`
	* creates a [local kind cluster](https://github.com/aws/aws-node-termination-handler/blob/main/test/k8s-local-cluster-test/kind-three-node-cluster.yaml)

* `make eks-cluster-test`
  * creates an [eks cluster](https://github.com/aws/aws-node-termination-handler/blob/main/test/eks-cluster-test/cluster-spec.yaml)
  * *Note if testing Windows, `eks-cluster-test` must be used*

**Using Test Drivers**

Users can also kick off the tests by invoking the test driver scripts:
* **local cluster:** `./test/k8s-local-cluster-test/run-test`
* **eks cluster:** `./test/eks-cluster-test/run-test`

By invoking the test drivers directly, users will be able to pass in supported parameters to tailor their test run accordingly. For example,
use **-p when starting a local cluster test to PRESERVE the created cluster:** `./test/k8s-local-cluster-test/run-test -b e2e-test -d -p`

Whether the tests succeed or fail, the cluster will be preserved for further exploration. By default, the cluster will be deleted regardless of test status.

Once clusters are created, each test in [e2e folder](https://github.com/aws/aws-node-termination-handler/tree/main/test/e2e) will be executed sequentially until a test fails or all have succeeded.


#### Configuring
As noted in [eks-cluster-test/run-test](https://github.com/aws/aws-node-termination-handler/blob/main/test/eks-cluster-test/run-test#L23) a `CONFIG` file can be provided if users want to test on an existing eks cluster or use an existing ecr repo for supplying the Docker images. Users will need to invoke the test driver for eks-cluster-test directly to pass CONFIG as a param as detailed in the section above.


#### Example
Using [maintenance-event-cancellation-test](https://github.com/aws/aws-node-termination-handler/blob/main/test/e2e/maintenance-event-cancellation-test) as an example.

Keep in mind what NTH is expected to do: **...cordon the node to ensure no new work is scheduled there, then drain it, removing any existing work** - [NTH ReadMe](https://github.com/aws/aws-node-termination-handler)


**The Test**
* What happens when a cluster of spot instances receives a maintenance event for system-reboot, but then said event gets canceled?

**The Expectation**
* When NTH consumes the *system-reboot* event, it should cordon the node, ensure no new work is scheduled there, and remove any existing work.
* When the subsequent  *canceled* event is sent, NTH should make the node schedulable again.

**The Verification**
* After the *system-reboot* event, the test will verify that the node being rebooted has been cordoned, tainted properly (no new work to be scheduled), and any running pods are evicted (existing work is removed).
* Once the nodes and pods are in expected state, the *canceled* event is sent and the test verifies the node is no longer cordoned by checking its status AND scheduling another pod (new work) successfully.

**The Walkthrough**
* NTH and an empty test pod, regular-pod-test, are deployed to the cluster
  *  regular-pod-test is used to represent work scheduled onto the nodes
* Regular-pod-test should be scheduled and started on the worker node
* [EC2-Metadata-Mock](https://github.com/aws/amazon-ec2-metadata-mock) (aka AEMM; used to mock IMDS) is installed on the cluster
* When AEMM starts running, a new maintenance event for *system-reboot* will be sent and consumed by NTH.
* NTH consumes the event and forwards the *system-reboot* event to its [interruption channel](https://github.com/aws/aws-node-termination-handler/blob/18ce8fdd87172c5e774b3693b29ce62c49e93272/cmd/node-termination-handler.go#L152) triggering the cordoning of the node and draining of any pods
* Once nodes have been cordoned and pods drained, AEMM is restarted to send a *canceled* event instead of system-reboot
* NTH forwards the *canceled* event to its [cancel channel](https://github.com/aws/aws-node-termination-handler/blob/18ce8fdd87172c5e774b3693b29ce62c49e93272/cmd/node-termination-handler.go#L160) which proceeds with uncordoning and removing any taints
* If the node is uncordoned and taints were removed successfully, then new work (pods) should be schedulable to the node
* Therefore, the final check is ensuring regular-pod-test is rescheduled and deployed to the node
