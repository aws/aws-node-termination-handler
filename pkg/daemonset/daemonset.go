// Copyright 2016-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package daemonset

import (
	"context"

	"github.com/aws/aws-node-termination-handler/pkg/config"
	appv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/kubectl/pkg/describe"
)

type DaemonSet struct {
	daemonSetHelper v1.DaemonSetInterface
}

// New will construct a daemonset struct to perform various daemonset function through the kubernetes api server
func New(nthConfig config.Config, clientset *kubernetes.Clientset) *DaemonSet {
	describer := describe.DaemonSetDescriber{
		Interface: clientset,
	}

	daemonSetHelper := describer.AppsV1().DaemonSets(nthConfig.PodNamespace)

	return &DaemonSet{
		daemonSetHelper: daemonSetHelper,
	}
}

func (d *DaemonSet) GetOne(name string) (*appv1.DaemonSet, error) {
	return d.daemonSetHelper.Get(context.Background(), "aws-node-termination-handler", metav1.GetOptions{})
}
