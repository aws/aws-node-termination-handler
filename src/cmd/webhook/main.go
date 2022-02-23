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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/webhook"
	"knative.dev/pkg/webhook/certificates"
	"knative.dev/pkg/webhook/resourcesemantics"
	"knative.dev/pkg/webhook/resourcesemantics/defaulting"
	"knative.dev/pkg/webhook/resourcesemantics/validation"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var resources = map[schema.GroupVersionKind]resourcesemantics.GenericCRD{
	v1alpha1.GroupVersion.WithKind("Terminator"): &v1alpha1.Terminator{},
}

func main() {
	var servicePortStr string
	var serviceName string
	flag.StringVar(&servicePortStr, "port", os.Getenv("SERVICE_PORT"), "Listen on port.")
	flag.StringVar(&serviceName, "service-name", os.Getenv("SERVICE_NAME"), "Service name for the dynamic webhook certificate.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	servicePort := 8443
	if servicePortStr != "" {
		servicePort = parseIntOrDie(servicePortStr)
	}

	config := knativeinjection.ParseAndGetRESTConfigOrDie()
	ctx := webhook.WithOptions(knativeinjection.WithNamespaceScope(signals.NewContext(), system.Namespace()), webhook.Options{
		Port:        servicePort,
		ServiceName: serviceName,
		SecretName:  fmt.Sprintf("%s-cert", serviceName),
	})

	sharedmain.MainWithConfig(
		ctx,
		"webhook", // componentName
		config,
		certificates.NewController,
		newCRDDefaultingWebhook,
		newCRDValidationWebhook,
	)
}

func newCRDDefaultingWebhook(ctx context.Context, w configmap.Watcher) *controller.Impl {
	return defaulting.NewAdmissionController(
		ctx,
		"defaulting.webhook.terminators.k8s.aws", // name
		"/default-resource",                      // path
		resources,                                // handler
		nil,                                      // withContext (func)
		true,                                     // disallowUnknownFields
	)
}

func newCRDValidationWebhook(ctx context.Context, w configmap.Watcher) *controller.Impl {
	return validation.NewAdmissionController(
		ctx,
		"validation.webhook.terminators.k8s.aws", // name
		"/validate-resource",                     // path
		resources,                                // handlers
		nil,                                      // withContext (func)
		true,                                     // disallowUnknownFields
	)
}

func parseIntOrDie(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return i
}
