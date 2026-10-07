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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/go-logr/logr"

	"github.com/rossigee/provider-signoz/apis/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := v1beta1.SchemeBuilder.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add v1beta1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	return scheme
}

func TestReconcile_SuccessfulProbe(t *testing.T) {
	probeGotCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/channels" {
			t.Errorf("Expected path /api/v1/channels, got %s", r.URL.Path)
		}
		if r.Header.Get("SIGNOZ-API-KEY") == "" {
			t.Error("Expected SIGNOZ-API-KEY header")
		}
		probeGotCalled = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []interface{}{}})
	}))
	defer server.Close()

	scheme := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "signoz-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"credentials": []byte(`{"apiKey":"valid-key"}`),
		},
	}
	pc := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pc",
		},
		Spec: v1beta1.ProviderConfigSpec{
			Endpoint: &server.URL,
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "signoz-creds",
							Namespace: "default",
						},
						Key: "credentials",
					},
				},
			},
		},
	}

	k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, pc).Build()

	rec := &reconciler{
		kube:    k8s,
		logger:  logr.Discard(),
		cfg:     ReconcilerConfig{Logger: logr.Discard(), ConnTimeout: 5 * time.Second, NowFn: time.Now},
		failCnt: make(map[string]int),
	}

	_, err := rec.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pc"},
	})

	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Verify probe was actually called
	if !probeGotCalled {
		t.Error("Expected probe to call upstream endpoint")
	}
}

func TestReconcile_AuthFailure_InvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid API key"})
	}))
	defer server.Close()

	scheme := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "signoz-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"credentials": []byte(`{"apiKey":"invalid-key"}`),
		},
	}
	pc := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pc",
		},
		Spec: v1beta1.ProviderConfigSpec{
			Endpoint: &server.URL,
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "signoz-creds",
							Namespace: "default",
						},
						Key: "credentials",
					},
				},
			},
		},
	}

	k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, pc).Build()

	rec := &reconciler{
		kube:    k8s,
		logger:  logr.Discard(),
		cfg:     ReconcilerConfig{Logger: logr.Discard(), ConnTimeout: 5 * time.Second, NowFn: time.Now},
		failCnt: make(map[string]int),
	}

	result, err := rec.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pc"},
	})

	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Auth failure should requeue
	if result.RequeueAfter == 0 {
		t.Error("Expected requeue after auth failure")
	}
}

func TestReconcile_EmptyAPIKey(t *testing.T) {
	scheme := newTestScheme(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "signoz-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"credentials": []byte(`{}`),
		},
	}
	pc := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pc",
		},
		Spec: v1beta1.ProviderConfigSpec{
			Endpoint: stringPtr("https://example.com"),
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "signoz-creds",
							Namespace: "default",
						},
						Key: "credentials",
					},
				},
			},
		},
	}

	k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, pc).Build()

	rec := &reconciler{
		kube:    k8s,
		logger:  logr.Discard(),
		cfg:     ReconcilerConfig{Logger: logr.Discard(), ConnTimeout: 5 * time.Second, NowFn: time.Now},
		failCnt: make(map[string]int),
	}

	result, err := rec.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pc"},
	})

	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Should requeue on empty credentials
	if result.RequeueAfter == 0 {
		t.Error("Expected requeue after empty credentials")
	}
}

func TestReconcile_SecretMissing(t *testing.T) {
	scheme := newTestScheme(t)
	pc := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pc",
		},
		Spec: v1beta1.ProviderConfigSpec{
			Endpoint: stringPtr("https://example.com"),
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      "nonexistent",
							Namespace: "default",
						},
						Key: "credentials",
					},
				},
			},
		},
	}

	k8s := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pc).Build()

	rec := &reconciler{
		kube:    k8s,
		logger:  logr.Discard(),
		cfg:     ReconcilerConfig{Logger: logr.Discard(), ConnTimeout: 5 * time.Second, NowFn: time.Now},
		failCnt: make(map[string]int),
	}

	result, err := rec.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-pc"},
	})

	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	// Should requeue on missing secret
	if result.RequeueAfter == 0 {
		t.Error("Expected requeue after missing secret")
	}
}

func TestInvalidCredentialsCondition_AuthError(t *testing.T) {
	cond := invalidCredentialsCondition(clients.ErrAuth, 10*time.Second)
	if cond.Reason != ReasonCredentialsRejected {
		t.Errorf("Expected ReasonCredentialsRejected, got %s", cond.Reason)
	}
	if cond.Status != corev1.ConditionFalse {
		t.Errorf("Expected ConditionFalse, got %v", cond.Status)
	}
}

func TestInvalidCredentialsCondition_TransientError(t *testing.T) {
	cond := invalidCredentialsCondition(clients.ErrTransient, 10*time.Second)
	if cond.Reason != ReasonUpstreamTransient {
		t.Errorf("Expected ReasonUpstreamTransient, got %s", cond.Reason)
	}
	if cond.Status != corev1.ConditionFalse {
		t.Errorf("Expected ConditionFalse, got %v", cond.Status)
	}
}

func TestInvalidCredentialsCondition_EmptyAPIKey(t *testing.T) {
	cond := invalidCredentialsCondition(
		errors.New("signoz credentials are missing an apiKey (misconfigured ProviderConfig or empty secret)"),
		10*time.Second)
	if cond.Reason != ReasonCredentialsEmpty {
		t.Errorf("Expected ReasonCredentialsEmpty, got %s", cond.Reason)
	}
}

func TestInvalidCredentialsCondition_ShortAPIKey(t *testing.T) {
	cond := invalidCredentialsCondition(
		errors.New("signoz credentials apiKey is shorter than the configured minimum"),
		10*time.Second)
	if cond.Reason != ReasonCredentialsShort {
		t.Errorf("Expected ReasonCredentialsShort, got %s", cond.Reason)
	}
}

func TestInvalidCredentialsCondition_EndpointUnreachable(t *testing.T) {
	cond := invalidCredentialsCondition(
		errors.New("connection refused: no such host"),
		10*time.Second)
	if cond.Reason != ReasonEndpointUnreachable {
		t.Errorf("Expected ReasonEndpointUnreachable, got %s", cond.Reason)
	}
}

func TestFingerprintKey(t *testing.T) {
	// Ensure fingerprint is consistent
	fp1 := fingerprintKey("https://api.signoz.cloud", "secret-key-123")
	fp2 := fingerprintKey("https://api.signoz.cloud", "secret-key-123")
	if fp1 != fp2 {
		t.Error("Fingerprint should be deterministic")
	}

	// Different inputs should produce different fingerprints
	fp3 := fingerprintKey("https://api.signoz.cloud", "different-key")
	if fp1 == fp3 {
		t.Error("Different keys should produce different fingerprints")
	}

	// Fingerprint should be masked (truncated to 16 chars)
	if len(fp1) != 16 {
		t.Errorf("Expected fingerprint length 16, got %d", len(fp1))
	}
}

func TestFailureTracking(t *testing.T) {
	rec := &reconciler{
		failCnt: make(map[string]int),
	}

	key := "test-pc"

	// Track failures
	rec.recordFailure(key)
	rec.recordFailure(key)
	if rec.failCnt[key] != 2 {
		t.Errorf("Expected 2 failures, got %d", rec.failCnt[key])
	}

	// Record success resets count
	rec.recordSuccess(key)
	if _, exists := rec.failCnt[key]; exists {
		t.Error("Expected failure count to be deleted on success")
	}
}

func stringPtr(s string) *string {
	return &s
}
