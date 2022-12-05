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

package main

import (
	"context"
	"flag"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"knative.dev/pkg/configmap/informer"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	knativelogging "knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"

	nodev1alpha1 "github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/pkg/event"
	asgterminateeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/asgterminate/v1"
	asgterminateeventv2 "github.com/aws/aws-node-termination-handler/pkg/event/asgterminate/v2"
	rebalancerecommendationeventv0 "github.com/aws/aws-node-termination-handler/pkg/event/rebalancerecommendation/v0"
	scheduledchangeeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/scheduledchange/v1"
	spotinterruptioneventv1 "github.com/aws/aws-node-termination-handler/pkg/event/spotinterruption/v1"
	statechangeeventv1 "github.com/aws/aws-node-termination-handler/pkg/event/statechange/v1"
	"github.com/aws/aws-node-termination-handler/pkg/logging"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	kubectlcordondrainer "github.com/aws/aws-node-termination-handler/pkg/node/cordondrain/kubectl"
	nodename "github.com/aws/aws-node-termination-handler/pkg/node/name"
	"github.com/aws/aws-node-termination-handler/pkg/sqsmessage"
	"github.com/aws/aws-node-termination-handler/pkg/terminator"
	terminatoradapter "github.com/aws/aws-node-termination-handler/pkg/terminator/adapter"
	"github.com/aws/aws-node-termination-handler/pkg/webhook"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/go-logr/zapr"
	//+kubebuilder:scaffold:imports
)

const componentName = "controller"

var scheme = runtime.NewScheme()

type Options struct {
	AWSRegion            string
	MetricsAddress       string
	EnableLeaderElection bool
	ProbeAddress         string
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(nodev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	options := parseOptions()
	ctrlConfig := ctrl.GetConfigOrDie()
	ctrlConfig.UserAgent = "aws-node-termination-handler"
	ctx, startInformers := knativeinjection.EnableInjectionOrDie(signals.NewContext(), ctrlConfig)
	logger, atomicLevel := sharedmain.SetupLoggerOrDie(ctx, componentName)
	ctx = logging.WithLogger(ctx, logger)
	clientSet := kubernetes.NewForConfigOrDie(ctrlConfig)
	cmw := informer.NewInformedWatcher(clientSet, system.Namespace())

	ctrl.SetLogger(zapr.NewLogger(logger.Desugar()))
	rest.SetDefaultWarningHandler(&knativelogging.WarningHandler{Logger: logger})
	sharedmain.WatchLoggingConfigOrDie(ctx, cmw, logger, atomicLevel, componentName)

	if err := cmw.Start(ctx.Done()); err != nil {
		logger.With("error", err).Fatal("failed to watch logging configuration")
	}
	startInformers()

	mgr, err := ctrl.NewManager(ctrlConfig, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     options.MetricsAddress,
		Port:                   9443,
		HealthProbeBindAddress: options.ProbeAddress,
		LeaderElection:         options.EnableLeaderElection,
		LeaderElectionID:       "aws-node-termination-handler.k8s.aws",
		Logger:                 zapr.NewLogger(logging.FromContext(ctx).Desugar()),
	})
	if err != nil {
		logger.With("error", err).Fatal("failed to create manager")
	}
	if err = indexNodeName(ctx, mgr.GetFieldIndexer()); err != nil {
		logger.With("error", err).
			With("type", "Pod").
			With("field", "spec.nodeName").
			Fatal("failed to add field index")
	}
	kubeClient := mgr.GetClient()

	awsConfig, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(options.AWSRegion))
	if err != nil {
		logger.With("error", err).Fatal("failed to load AWS configuration")
	}
	sqsClient := sqs.NewFromConfig(awsConfig)
	if sqsClient == nil {
		logger.Fatal("failed to create SQS client")
	}
	asgClient := autoscaling.NewFromConfig(awsConfig)
	if asgClient == nil {
		logger.Fatal("failed to create ASG client")
	}
	ec2Client := ec2.NewFromConfig(awsConfig)
	if ec2Client == nil {
		logger.Fatal("failed to create EC2 client")
	}

	eventParser := event.NewAggregatedParser(
		asgterminateeventv1.Parser{ASGLifecycleActionCompleter: asgClient},
		asgterminateeventv2.Parser{ASGLifecycleActionCompleter: asgClient},
		rebalancerecommendationeventv0.Parser{},
		scheduledchangeeventv1.Parser{},
		spotinterruptioneventv1.Parser{},
		statechangeeventv1.Parser{},
	)

	rec := terminator.Reconciler{
		Name:            "terminator",
		RequeueInterval: time.Duration(10) * time.Second,
		NodeGetterBuilder: terminatoradapter.NodeGetterBuilder{
			NodeGetter: node.Getter{KubeGetter: kubeClient},
		},
		NodeNameGetter: nodename.Getter{EC2InstancesDescriber: ec2Client},
		SQSClientBuilder: terminatoradapter.SQSMessageClientBuilder{
			SQSMessageClient: sqsmessage.Client{SQSClient: sqsClient},
		},
		SQSMessageParser: terminatoradapter.EventParser{Parser: eventParser},
		Getter:           terminatoradapter.Getter{KubeGetter: kubeClient},
		CordonDrainerBuilder: terminatoradapter.CordonDrainerBuilder{
			Builder: kubectlcordondrainer.Builder{
				ClientSet: clientSet,
				Cordoner:  kubectlcordondrainer.DefaultCordoner,
				Drainer:   kubectlcordondrainer.DefaultDrainer,
			},
		},
		WebhookClientBuilder: terminatoradapter.WebhookClientBuilder(
			webhook.ClientBuilder(webhook.NewHttpClientDo).NewClient,
		),
	}
	if err = rec.BuildController(
		ctrl.NewControllerManagedBy(mgr).
			WithOptions(controller.Options{MaxConcurrentReconciles: 10}),
	); err != nil {
		logger.With("error", err).
			With("name", rec.Name).
			Fatal("failed to create controller")
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.With("error", err).Fatal("failed to set up health check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.With("error", err).Fatal("failed to set up ready check")
	}

	if err := mgr.Start(ctx); err != nil {
		logger.With("error", err).Fatal("failure from manager")
	}
}

func parseOptions() Options {
	options := Options{}

	flag.StringVar(&options.AWSRegion, "aws-region", os.Getenv("AWS_REGION"), "The AWS region for API calls.")
	flag.StringVar(&options.MetricsAddress, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&options.ProbeAddress, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&options.EnableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	return options
}

func indexNodeName(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &v1.Pod{}, "spec.nodeName", func(o client.Object) []string {
		if o == nil {
			return nil
		}
		return []string{o.(*v1.Pod).Spec.NodeName}
	})
}
