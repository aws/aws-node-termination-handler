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

package kubectl

import (
	"context"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/node/cordondrain"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
)

type (
	Cordoner interface {
		Cordon(context.Context, *v1.Node, drain.Helper) error
	}

	Drainer interface {
		Drain(context.Context, *v1.Node, drain.Helper) error
	}

	Builder struct {
		Cordoner
		Drainer

		ClientSet kubernetes.Interface
	}
)

func (b Builder) Build(config cordondrain.Config) (CordonDrainer, error) {
	return CordonDrainer{
		Cordoner: b.Cordoner,
		Drainer:  b.Drainer,
		Helper: drain.Helper{
			Client:              b.ClientSet,
			Force:               config.Force,
			GracePeriodSeconds:  config.GracePeriodSeconds,
			IgnoreAllDaemonSets: config.IgnoreAllDaemonSets,
			DeleteEmptyDirData:  config.DeleteEmptyDirData,
			Timeout:             time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}, nil
}
