/*
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
	danav1 "github.com/dana-team/hns/api/v1"
	"github.com/dana-team/hns/internals/controllers"
	"github.com/dana-team/hns/internals/namespaceDB"
	"github.com/dana-team/hns/internals/server"
	"github.com/dana-team/hns/internals/webhooks"
	buildv1 "github.com/openshift/api/build/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	templatev1 "github.com/openshift/api/template/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(danav1.AddToScheme(scheme))
	utilruntime.Must(quotav1.AddToScheme(scheme))
	utilruntime.Must(templatev1.AddToScheme(scheme))
	utilruntime.Must(buildv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
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

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c1382367.dana.io",
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,

		// uncomment to debug locally
		//WebhookServer: webhook.NewServer(webhook.Options{CertDir: "k8s-webhook-server/serving-certs/"}),
	})

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	var ndb *namespaceDB.NamespaceDB
	ndb, err = namespaceDB.InitDB(scheme, setupLog.WithName("InitDB Logger"))
	if err != nil {
		setupLog.Error(err, "unable to successfully initialize namespaceDB")
		os.Exit(1)
	}

	setupLog.Info("setting up reconcilers")
	if err := controllers.SetupControllers(mgr, ndb); err != nil {
		setupLog.Error(err, "unable to successfully set up controllers")
		os.Exit(1)
	}

	setupLog.Info("setting up webhooks")
	webhooks.SetupWebhooks(mgr, ndb, scheme)
	// +kubebuilder:scaffold:builder

	ds := server.NewDiagramServer(mgr.GetClient())
	go ds.Run()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
