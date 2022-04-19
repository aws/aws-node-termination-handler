/*
Copyright 2022 Amazon.com, Inc. or its affiliates. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package adapter

import (
	"context"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
	v1 "k8s.io/api/core/v1"
)

type (
	NodeGetter interface {
		GetNode(context.Context, string, map[string]string) (*v1.Node, error)
	}

	NodeGetterBuilder struct {
		NodeGetter
	}

	nodeGetter struct {
		NodeGetter

		Labels map[string]string
	}
)

func (n NodeGetterBuilder) NewNodeGetter(terminator *v1alpha1.Terminator) terminator.NodeGetter {
	return nodeGetter{
		NodeGetter: n.NodeGetter,
		Labels:     terminator.Spec.MatchLabels,
	}
}

func (n nodeGetter) GetNode(ctx context.Context, nodeName string) (*v1.Node, error) {
	return n.NodeGetter.GetNode(ctx, nodeName, n.Labels)
}
