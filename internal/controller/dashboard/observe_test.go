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

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/rossigee/provider-signoz/apis/dashboard/v1beta1"
	"github.com/rossigee/provider-signoz/internal/clients"
)

// derivedID is what GenerateExternalName produces for crossplane-system / this
// dashboard name. It is deliberately a value that does not exist in SigNoz, so
// the test only passes if adoption actually happens.
const (
	testDashboardName  = "coredns-monitoring"
	testDashboardTitle = "CoreDNS Monitoring"
	testDerivedID      = "6d8498e1-620b-7f61-a01d-a12eba4fec52"
	testRealID         = "01a013bd-b238-7575-9b82-2a028e9be2ba"
)

// signozStub answers the two calls Observe makes when the recorded id is
// missing: the GET by id, which 404s, and the list, which reports the real
// dashboard under the slug of its title.
func signozStub(t *testing.T, listName, displayName string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v2/dashboards/" + testDerivedID:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "error",
				"error": map[string]interface{}{
					"type": "not-found", "code": "not_found", "message": "dashboard not found",
				},
			})

		case "/api/v2/dashboards":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"total": 1,
					"dashboards": []map[string]interface{}{
						{
							"id": testRealID, "name": listName, "schemaVersion": "v6",
							"spec": map[string]interface{}{
								"display": map[string]interface{}{"name": displayName},
							},
						},
					},
				},
			})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	if err := v1beta1.AddToScheme(s); err != nil {
		t.Fatalf("cannot build scheme: %v", err)
	}
	return s
}

func testDashboard() *v1beta1.Dashboard {
	return &v1beta1.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testDashboardName,
			Namespace:   "crossplane-system",
			Annotations: map[string]string{"crossplane.io/external-name": testDerivedID},
		},
		Spec: v1beta1.DashboardSpec{
			ForProvider: v1beta1.DashboardParameters{Title: testDashboardTitle},
		},
	}
}

// A Dashboard that lost its external-name must be adopted, not recreated, and
// the discovered id must be written back to the API server. Persisting is the
// part that regressed: the reconciler writes only the status subresource once a
// resource is up to date, so an annotation set in Observe and not written here
// is silently discarded and adoption repeats forever.
func TestObserveAdoptsBySlugAndPersistsID(t *testing.T) {
	srv := signozStub(t, "coredns-monitoring", testDashboardTitle)
	defer srv.Close()

	cr := testDashboard()

	kube := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(cr).
		Build()

	e := &external{
		service: clients.NewClient(clients.Config{BaseURL: srv.URL, APIKey: "test-key"}),
		kube:    resource.ClientApplicator{Client: kube},
	}

	got, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}

	if !got.ResourceExists {
		t.Fatal("Expected ResourceExists true - the dashboard is present in SigNoz")
	}

	if cr.Status.AtProvider.ID != testRealID {
		t.Errorf("Expected atProvider.id %s, got %s", testRealID, cr.Status.AtProvider.ID)
	}

	// Re-read from the API server: the annotation on the in-memory object proves
	// nothing, since Observe is handed a copy the reconciler may discard.
	stored := &v1beta1.Dashboard{}
	if err := kube.Get(context.Background(),
		types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, stored); err != nil {
		t.Fatalf("cannot re-read dashboard: %v", err)
	}

	if name := stored.GetAnnotations()["crossplane.io/external-name"]; name != testRealID {
		t.Errorf("Expected persisted external-name %s, got %s", testRealID, name)
	}
}

// Adoption must match the slug SigNoz actually stores, not the raw title. This
// is the bug that made v0.6.5 inert: it compared the title verbatim, so
// "CoreDNS Monitoring" never matched the stored "coredns-monitoring".
func TestObserveAdoptsWhenOnlyTheSlugMatches(t *testing.T) {
	// Stored name deliberately differs from both the title and the CR name.
	srv := signozStub(t, "kubernetes-cluster-overview", "Kubernetes Cluster Overview")
	defer srv.Close()

	cr := testDashboard()
	cr.Spec.ForProvider.Title = "Kubernetes Cluster Overview"
	cr.Name = "kubernetes-cluster"

	kube := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(cr).
		Build()

	e := &external{
		service: clients.NewClient(clients.Config{BaseURL: srv.URL, APIKey: "test-key"}),
		kube:    resource.ClientApplicator{Client: kube},
	}

	got, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}

	if !got.ResourceExists {
		t.Fatal("Expected adoption via the slug of the title")
	}

	if cr.Status.AtProvider.ID != testRealID {
		t.Errorf("Expected atProvider.id %s, got %s", testRealID, cr.Status.AtProvider.ID)
	}
}

// When no dashboard matches, Observe must report absence so the reconciler
// creates one. It must not adopt something arbitrary.
func TestObserveReportsAbsentWhenNoNameMatches(t *testing.T) {
	srv := signozStub(t, "something-else", "Something Else")
	defer srv.Close()

	cr := testDashboard()

	kube := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(cr).
		Build()

	e := &external{
		service: clients.NewClient(clients.Config{BaseURL: srv.URL, APIKey: "test-key"}),
		kube:    resource.ClientApplicator{Client: kube},
	}

	got, err := e.Observe(context.Background(), cr)
	if err != nil {
		t.Fatalf("Observe failed: %v", err)
	}

	if got.ResourceExists {
		t.Error("Expected ResourceExists false - nothing matches the title or its slug")
	}
}

// A failure to search is not evidence the dashboard is gone. Reporting absent
// would send the reconciler into Create and wedge the resource on a blip.
func TestObserveReportsErrorWhenSearchFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  map[string]interface{}{"type": "internal", "code": "internal", "message": "boom"},
		})
	}))
	defer srv.Close()

	cr := testDashboard()

	kube := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(cr).
		Build()

	e := &external{
		service: clients.NewClient(clients.Config{BaseURL: srv.URL, APIKey: "test-key"}),
		kube:    resource.ClientApplicator{Client: kube},
	}

	got, err := e.Observe(context.Background(), cr)
	if err == nil {
		t.Fatal("Expected an error when the dashboard search fails")
	}

	if got.ResourceExists {
		t.Error("A failed search must not be reported as absence")
	}
}
