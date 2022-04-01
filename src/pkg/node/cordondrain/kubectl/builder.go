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
	"fmt"
	"time"

	"github.com/aws/aws-node-termination-handler/pkg/node/cordondrain"
	"go.uber.org/multierr"

	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
)

type builder struct {
	kubernetes.Interface
	Cordoner
	Drainer
}

func NewBuilder(kubeClient kubernetes.Interface, cordoner Cordoner, drainer Drainer) (cordondrain.Builder, error) {
	var err error
	if kubeClient == nil {
		err = multierr.Append(err, fmt.Errorf("argument 'kubeClient' is nil"))
	}
	if cordoner == nil {
		err = multierr.Append(err, fmt.Errorf("argument 'cordoner' is nil"))
	}
	if drainer == nil {
		err = multierr.Append(err, fmt.Errorf("argument 'drainer' is nil"))
	}
	if err != nil {
		return nil, err
	}

	return builder{
		Cordoner:  cordoner,
		Drainer:   drainer,
		Interface: kubeClient,
	}, nil
}

func (c builder) Build(config cordondrain.Config) (cordondrain.CordonDrainer, error) {
	helper := drain.Helper{
		Client:              c,
		Force:               config.Force,
		GracePeriodSeconds:  config.GracePeriodSeconds,
		IgnoreAllDaemonSets: config.IgnoreAllDaemonSets,
		DeleteEmptyDirData:  config.DeleteEmptyDirData,
		Timeout:             time.Duration(config.TimeoutSeconds) * time.Second,
	}
	return NewCordonDrainer(helper, c.Cordoner, c.Drainer)
}
