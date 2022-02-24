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
	"flag"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"knative.dev/pkg/configmap/informer"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	nodev1alpha1 "github.com/aws/aws-node-termination-handler/api/v1alpha1"
	"github.com/aws/aws-node-termination-handler/controllers"
	"github.com/go-logr/zapr"
	//+kubebuilder:scaffold:imports
)

const component = "controller"

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(nodev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	config := ctrl.GetConfigOrDie()
	config.UserAgent = "aws-node-termination-handler"
	ctx, startInformers := knativeinjection.EnableInjectionOrDie(signals.NewContext(), config)
	rootLogger, atomicLevel := sharedmain.SetupLoggerOrDie(ctx, component)
	ctx = logging.WithLogger(ctx, rootLogger)
	ctrlLogger := rootLogger.With("component", component)
	cmw := informer.NewInformedWatcher(kubernetes.NewForConfigOrDie(config), system.Namespace())

	ctrl.SetLogger(zapr.NewLogger(rootLogger.Desugar()))
	rest.SetDefaultWarningHandler(&logging.WarningHandler{Logger: rootLogger})
	sharedmain.WatchLoggingConfigOrDie(ctx, cmw, rootLogger, atomicLevel, component)

	if err := cmw.Start(ctx.Done()); err != nil {
		ctrlLogger.With("error", err).Fatal("failed to watch logging configuration")
	}
	startInformers()

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "aws-node-termination-handler.k8s.aws",
		Logger:                 zapr.NewLogger(logging.FromContext(ctx).Desugar()),
	})
	if err != nil {
		ctrlLogger.With("error", err).Fatal("unable to start manager")
	}

	if err = (&controllers.TerminatorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		ctrlLogger.With("error", err).Fatalw("unable to create controller")
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		ctrlLogger.With("error", err).Fatal("unable to set up health check")
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		ctrlLogger.With("error", err).Fatal("unable to set up ready check")
	}

	ctrlLogger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrlLogger.With("error", err).Fatal("problem running manager")
	}
}
