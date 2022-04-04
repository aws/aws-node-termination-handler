/*
Copyright 2022.

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
	"github.com/aws/aws-node-termination-handler/pkg/logging"

	"k8s.io/apimachinery/pkg/api/errors"
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

func (g Getter) GetTerminator(ctx context.Context, name types.NamespacedName) (*v1alpha1.Terminator, error) {
	terminator := &v1alpha1.Terminator{}
	if err := g.Get(ctx, name, terminator); err != nil {
		if errors.IsNotFound(err) {
			logging.FromContext(ctx).Warn("terminator not found")
			return nil, nil
		}

		logging.FromContext(ctx).
			With("error", err).
			Error("failed to retrieve terminator")
		return nil, err
	}

	return terminator, nil
}
