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

package providerconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	"github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
)

const controllerName = "providerconfig.signoz.crossplane.io"

// Condition type for credentials validity, set on ProviderConfig.status.
//
// Distinct from xpv1.TypeReady so operators can grep for a single source of
// truth when investigating "is my ProviderConfig healthy?" — the crossplane
// convention of using TypeReady/Available for PC status conflates
// "controller-reachable" and "credentials-valid".
const (
	TypeCredentialsValid xpv1.ConditionType = "CredentialsValid"

	ReasonCredentialsAccepted  = "CredentialsAccepted"
	ReasonCredentialsRejected  = "CredentialsRejected"
	ReasonCredentialsEmpty     = "CredentialsEmpty"
	ReasonCredentialsShort     = "CredentialsTooShort"
	ReasonUpstreamTransient    = "UpstreamTransient"
	ReasonSecretMissing        = "SecretMissing"
	ReasonEndpointUnreachable  = "EndpointUnreachable"
)

// Backoff constants. Requeue intervals come from clients.BackoffPolicy; these
// are absolute safety nets for error paths where err==nil (e.g. status-write
// failures).
const (
	authRequeueMin = 30 * time.Second
)

// ReconcilerConfig bundles tunables for the ProviderConfig reconciler. Wired
// from main() via SetupWithConfig so tests can use a fixed timer.
type ReconcilerConfig struct {
	Logger logr.Logger
	// ConnTimeout for the credentials probe. Sub-probe budget for each
	// attempt so a slow upstream does not stall the controller's
	// workqueue.
	ConnTimeout time.Duration
	// BackoffPolicy drives the next requeue interval. Defaults to
	// clients.DefaultBackoffPolicy.
	BackoffPolicy clients.BackoffPolicy
	// NowFn returns the current time; overridable for tests.
	NowFn func() time.Time
}

// Setup registers the ProviderConfig controller with default knobs.
func Setup(mgr ctrl.Manager) error {
	return SetupWithConfig(mgr, ReconcilerConfig{
		Logger:        mgr.GetLogger(),
		ConnTimeout:   10 * time.Second,
		BackoffPolicy: clients.DefaultBackoffPolicy,
		NowFn:         time.Now,
	})
}

// SetupWithConfig registers the ProviderConfig controller with the supplied
// configuration. Used by main.go to bind CLI-supplied tunables.
func SetupWithConfig(mgr ctrl.Manager, cfg ReconcilerConfig) error {
	r := &reconciler{
		kube:    mgr.GetClient(),
		logger:  cfg.Logger,
		cfg:     cfg,
		failCnt: make(map[string]int),
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&v1beta1.ProviderConfig{}).
		Complete(r)
}

type reconciler struct {
	kube   client.Client
	logger logr.Logger
	cfg    ReconcilerConfig

	// failCnt tracks consecutive credentials-probe failures per ProviderConfig.
	// Keyed by namespaced name. Reset on a successful probe.
	mu      sync.Mutex
	failCnt map[string]int
}

// Reconcile verifies ProviderConfig credentials are accepted by the upstream
// Signoz API and reports the result in the CredentialsValid condition.
//
// Behaviour:
//   - On a 2xx probe: TypeReady=Available + CredentialsValid=True
//   - On an auth-class error: CredentialsValid=False (CredentialsRejected /
//     CredentialsEmpty / CredentialsTooShort / UpstreamTransient with reason)
//   - On a transient error: CredentialsValid=False (UpstreamTransient);
//     requeue is shorter to recover quickly when upstream comes back.
//
// The reconciler stays Running even when credentials are invalid — it just
// requeues on exp backoff, never CrashLoops, so this PC "stay and report"
// behaviour matches the operator contract.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.logger.WithValues("providerconfig", req.Name)

	pc := &v1beta1.ProviderConfig{}
	if err := r.kube.Get(ctx, req.NamespacedName, pc); err != nil {
		log.Error(err, "failed to get ProviderConfig")
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Build a client from the ProviderConfig and probe credentials. We
	// deliberately use clients.GetConfig (single-source-of-truth) so any
	// validation drift between probe and Observe is impossible.
	probeCtx, cancel := context.WithTimeout(ctx, r.cfg.ConnTimeout)
	defer cancel()
	cfg, err := clients.GetConfigFromProviderConfig(probeCtx, r.kube, pc)
	if err != nil {
		log.Error(err, "failed to extract credentials from ProviderConfig")
		r.recordFailure(req.NamespacedName.String())
		pc.Status.SetConditions(invalidCredentialsCondition(err, r.cfg.ConnTimeout))
		return r.updateStatusAndRequeue(ctx, pc, log, err)
	}

	c := clients.NewClient(*cfg)
	if err := c.ProbeCredentials(probeCtx); err != nil {
		log.Error(err, "credentials probe failed",
			"endpoint", cfg.BaseURL,
			"fingerprint", fingerprintKey(cfg.BaseURL, cfg.APIKey))
		r.recordFailure(req.NamespacedName.String())
		pc.Status.SetConditions(invalidCredentialsCondition(err, r.cfg.ConnTimeout))
		return r.updateStatusAndRequeue(ctx, pc, log, err)
	}

	// Probe succeeded.
	r.recordSuccess(req.NamespacedName.String())
	log.Info("ProviderConfig credentials verified", "endpoint", cfg.BaseURL)
	pc.Status.SetConditions(xpv1.Available())
	pc.Status.SetConditions(xpv1.Condition{
		Type:    TypeCredentialsValid,
		Status:  corev1.ConditionTrue,
		Reason:  ReasonCredentialsAccepted,
		Message: "Credentials accepted by upstream Signoz API.",
	})

	if err := r.writeStatus(ctx, pc); err != nil {
		return reconcile.Result{RequeueAfter: authRequeueMin}, nil
	}
	return reconcile.Result{}, nil
}

// updateStatusAndRequeue pushes the status update and returns a requeue delay
// derived from the upstream error class via clients.Backoff.
func (r *reconciler) updateStatusAndRequeue(ctx context.Context, pc *v1beta1.ProviderConfig, log logr.Logger, err error) (reconcile.Result, error) {
	if werr := r.writeStatus(ctx, pc); werr != nil {
		return reconcile.Result{RequeueAfter: authRequeueMin}, nil
	}
	r.mu.Lock()
	failures := r.failCnt[pc.GetNamespace()+"/"+pc.GetName()]
	r.mu.Unlock()
	class, delay := clients.Backoff(err, failures, r.cfg.BackoffPolicy)
	log.Info("ProviderConfig credentials probe failed; requeueing",
		"consecutive_failures", failures,
		"class", classString(class),
		"in", delay.String())
	if delay <= 0 {
		delay = authRequeueMin
	}
	return reconcile.Result{RequeueAfter: delay}, nil
}

func classString(c clients.BackoffClass) string {
	switch c {
	case clients.ClassAuth:
		return "auth"
	case clients.ClassTransient:
		return "transient"
	case clients.ClassRateLimited:
		return "rate-limited"
	default:
		return "none"
	}
}

func (r *reconciler) writeStatus(ctx context.Context, pc *v1beta1.ProviderConfig) error {
	fresh := &v1beta1.ProviderConfig{}
	if err := r.kube.Get(ctx, client.ObjectKey{Name: pc.GetName()}, fresh); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		r.logger.Error(err, "failed to re-fetch ProviderConfig for status update")
		return err
	}
	fresh.Status = pc.Status
	if err := r.kube.Status().Update(ctx, fresh); err != nil {
		if k8serrors.IsConflict(err) {
			return err
		}
		r.logger.Error(err, "failed to update ProviderConfig status")
		return err
	}
	return nil
}

func (r *reconciler) recordFailure(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCnt[key]++
}

func (r *reconciler) recordSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failCnt, key)
}

// invalidCredentialsCondition maps an error from credentials extraction or
// probe to a typed Condition. Reason selection encodes the upstream error
// class so operators can route alerts on individual conditions rather than
// scraping event strings.
func invalidCredentialsCondition(err error, probeTimeout time.Duration) xpv1.Condition {
	c := xpv1.Condition{
		Type:   TypeCredentialsValid,
		Status: corev1.ConditionFalse,
	}

	switch {
	case stderrors.Is(err, clients.ErrAuth):
		c.Reason = ReasonCredentialsRejected
		c.Message = fmt.Sprintf("Upstream rejected credentials: %v", err)
	case stderrors.Is(err, clients.ErrTransient):
		c.Reason = ReasonUpstreamTransient
		c.Message = fmt.Sprintf("Upstream Signoz API is transiently unavailable: %v (probe timeout %s)", err, probeTimeout)
	default:
		switch err.Error() {
		case "cannot extract credentials":
			c.Reason = ReasonSecretMissing
			c.Message = "Failed to look up credentials Secret / InjectedIdentity / Filesystem source referenced by ProviderConfig"
		case "cannot unmarshal signoz credentials as JSON":
			c.Reason = ReasonSecretMissing
			c.Message = "Credentials source exists but is not valid JSON or is missing the apiKey field"
		default:
			switch {
			case contains(err.Error(), "apiKey"):
				if contains(err.Error(), "empty") || contains(err.Error(), "missing") {
					c.Reason = ReasonCredentialsEmpty
				} else {
					c.Reason = ReasonCredentialsShort
				}
				c.Message = err.Error()
			case contains(err.Error(), "no such host") || contains(err.Error(), "i/o timeout") || contains(err.Error(), "connection refused"):
				c.Reason = ReasonEndpointUnreachable
				c.Message = fmt.Sprintf("ProviderConfig endpoint unreachable: %v", err)
			default:
				c.Reason = ReasonCredentialsRejected
				c.Message = fmt.Sprintf("Credentials invalid: %v", err)
			}
		}
	}
	return c
}

// fingerprintKey returns a sha256-derived fingerprint of (baseURL, apiKey)
// suitable for log de-duplication so apiKey values are not written verbatim.
func fingerprintKey(baseURL, apiKey string) string {
	h := sha256.New()
	h.Write([]byte(baseURL))
	h.Write([]byte{0})
	h.Write([]byte(apiKey))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// contains is a small helper kept local to avoid cross-package imports in
// invalidCredentialsCondition. err.Error() strings are short, so linear scan
// is fine here.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
