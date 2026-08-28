package main

import (
	"flag"
	"os"
	"time"

	workloadsv1alpha1 "github.com/opensoha/soha-operator/api/v1alpha1"
	"github.com/opensoha/soha-operator/internal/controller"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	var metricsAddress string
	var probeAddress string
	var leaderElection bool
	flag.StringVar(&metricsAddress, "metrics-bind-address", ":8080", "Address for the metrics endpoint.")
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081", "Address for health probes.")
	flag.BoolVar(&leaderElection, "leader-elect", false, "Enable leader election.")
	zapOptions := newZapOptions()
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)).WithValues("service", "soha-operator"))
	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(workloadsv1alpha1.AddToScheme(scheme))

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                        scheme,
		Metrics:                       metricsserver.Options{BindAddress: metricsAddress},
		HealthProbeBindAddress:        probeAddress,
		LeaderElection:                leaderElection,
		LeaderElectionID:              "soha-operator.workloads.soha.io",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager", "event", "operator.manager.create_failed")
		os.Exit(1)
	}

	reconciler := &controller.WorkloadCronJobReconciler{Client: manager.GetClient(), Scheme: manager.GetScheme()}
	if err := reconciler.SetupWithManager(manager); err != nil {
		setupLog.Error(err, "unable to create WorkloadCronJob controller", "event", "operator.controller.create_failed")
		os.Exit(1)
	}
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to configure health check", "event", "operator.health_check.configure_failed")
		os.Exit(1)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to configure readiness check", "event", "operator.readiness_check.configure_failed")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "event", "operator.manager.starting")
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped with an error", "event", "operator.manager.failed")
		os.Exit(1)
	}
}

func newZapOptions() zap.Options {
	return zap.Options{
		Development: false,
		EncoderConfigOptions: []zap.EncoderConfigOption{func(config *zapcore.EncoderConfig) {
			config.TimeKey = "timestamp"
			config.NameKey = "component"
			config.MessageKey = "message"
		}},
		TimeEncoder: func(value time.Time, encoder zapcore.PrimitiveArrayEncoder) {
			zapcore.RFC3339NanoTimeEncoder(value.UTC(), encoder)
		},
	}
}
