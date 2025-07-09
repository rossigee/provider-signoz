/*
Copyright 2024 The Crossplane Authors.

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
	"os"
	"path/filepath"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/feature"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"

	"github.com/crossplane-contrib/provider-signoz/apis"
	alertcontroller "github.com/crossplane-contrib/provider-signoz/internal/controller/alert"
	channelcontroller "github.com/crossplane-contrib/provider-signoz/internal/controller/channel"
	dashboardcontroller "github.com/crossplane-contrib/provider-signoz/internal/controller/dashboard"
)

func main() {
	var (
		app = kingpin.New(filepath.Base(os.Args[0]), "SigNoz support for Crossplane.").DefaultEnvars()

		debug        = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
		syncInterval = app.Flag("sync", "Controller manager sync interval").Short('s').Default("1h").Duration()

		leaderElection          = app.Flag("leader-election", "Use leader election for the controller manager.").Short('l').Default("false").Envar("LEADER_ELECTION").Bool()
		leaderElectionNamespace = app.Flag("leader-election-namespace", "Namespace in which to create the leader election configmap.").Default("crossplane-system").Envar("LEADER_ELECTION_NAMESPACE").String()

		pollInterval              = app.Flag("poll", "Poll interval controls how often an individual resource should be checked for drift.").Default("10m").Duration()
		maxReconcileRate          = app.Flag("max-reconcile-rate", "The global maximum rate per second at which resources may checked for drift from the desired state.").Default("10").Int()
		enableManagementPolicies  = app.Flag("enable-management-policies", "Enable support for Management Policies.").Default("false").Envar("ENABLE_MANAGEMENT_POLICIES").Bool()
	)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	zl := zap.New(zap.UseDevMode(*debug))
	log := logging.NewLogrLogger(zl.WithName("provider-signoz"))
	if *debug {
		// The controller-runtime runs with a no-op logger by default. It is
		// *very* verbose even at info level, so we only provide it a real
		// logger when we're running in debug mode.
		ctrl.SetLogger(zl)
	}

	log.Debug("Starting", "sync-interval", syncInterval.String())

	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Info("Cannot get config", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Cache: cache.Options{
			SyncPeriod: syncInterval,
		},

		// controller-runtime uses both ConfigMaps and Leases for leader
		// election by default. Leases expire after 15 seconds, with a
		// 10 second renewal deadline. We've observed leader loss due to
		// renewal deadlines being exceeded when under high load - i.e.
		// hundreds of reconciles per second and ~200rps to the API
		// server. Switching to Leases only and longer leases appears to
		// alleviate this.
		LeaderElection:                *leaderElection,
		LeaderElectionID:              "crossplane-leader-election-provider-signoz",
		LeaderElectionResourceLock:    resourcelock.LeasesResourceLock,
		LeaderElectionNamespace:       *leaderElectionNamespace,
		LeaseDuration:                 func() *time.Duration { d := 60 * time.Second; return &d }(),
		RenewDeadline:                 func() *time.Duration { d := 50 * time.Second; return &d }(),
	})
	if err != nil {
		log.Info("Cannot create manager", "error", err)
		os.Exit(1)
	}

	o := controller.Options{
		Logger:                  log,
		MaxConcurrentReconciles: *maxReconcileRate,
		PollInterval:            *pollInterval,
		GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
		Features:                &feature.Flags{},
	}

	if *enableManagementPolicies {
		log.Info("Management policies feature enabled")
	}

	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Info("Cannot add SigNoz APIs to scheme", "error", err)
		os.Exit(1)
	}

	if err := dashboardcontroller.Setup(mgr, o); err != nil {
		log.Info("Cannot setup dashboard controller", "error", err)
		os.Exit(1)
	}

	if err := alertcontroller.Setup(mgr, o); err != nil {
		log.Info("Cannot setup alert controller", "error", err)
		os.Exit(1)
	}

	if err := channelcontroller.Setup(mgr, o); err != nil {
		log.Info("Cannot setup channel controller", "error", err)
		os.Exit(1)
	}

	// Start the manager
	log.Info("Starting controller manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Info("Cannot start controller manager", "error", err)
		os.Exit(1)
	}
}