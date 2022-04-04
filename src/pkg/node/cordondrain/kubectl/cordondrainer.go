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

package kubectl

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubectl/pkg/drain"
)

type CordonDrainer struct {
	Cordoner
	Drainer
	drain.Helper
}

func (c CordonDrainer) Cordon(ctx context.Context, node *v1.Node) error {
	return c.Cordoner.Cordon(ctx, node, c.Helper)
}

func (c CordonDrainer) Drain(ctx context.Context, node *v1.Node) error {
	return c.Drainer.Drain(ctx, node, c.Helper)
}
