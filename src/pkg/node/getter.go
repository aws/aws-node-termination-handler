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

package node

import (
	"context"

	"github.com/aws/aws-node-termination-handler/pkg/logging"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type (
	KubeGetter interface {
		Get(context.Context, client.ObjectKey, client.Object) error
	}

	Getter struct {
		KubeGetter
	}
)

func (g Getter) GetNode(ctx context.Context, nodeName string) (*v1.Node, error) {
	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).Named("node"))

	node := &v1.Node{}
	if err := g.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		logging.FromContext(ctx).
			With("error", err).
			Error("failed to retrieve node")
		return nil, err
	}

	return node, nil
}
