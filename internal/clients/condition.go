package clients

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
)

// TypeUpstreamAuth is a condition type managed-resource controllers set on
// the managed CR when an upstream 401/403 has been observed. Distinct from
// xpv1.TypeSynced so an upstream auth failure doesn't conflate with
// managed-resource drift detection.
//
// Operators can route alerts via
// `kubectl get <cr> -o jsonpath='{.status.conditions[?(@.type=="UpstreamAuth")]}'`.
const TypeUpstreamAuth xpv1.ConditionType = "UpstreamAuth"

const (
	ReasonUpstreamAuthRejected   = "Rejected"
	ReasonUpstreamAuthRecovering = "Recovering"
)

// RecordUpstreamCondition examines err and updates UpstreamAuth on the
// supplied managed resource's Status.Conditions. It is intended to be called
// from Observe/Create/Update/Delete paths before the error is wrapped.
//
// If err is nil and `clearOnSuccess` is true, any prior UpstreamAuth=False is
// removed (so the breaker-style "last seen: rejected" doesn't linger once the
// credentials are fixed).
func RecordUpstreamCondition(ctx context.Context, status *xpv1.ConditionedStatus, err error, clearOnSuccess bool) {
	lg := log.FromContext(ctx)
	class := Classify(err)
	switch class {
	case ClassAuth:
		status.SetConditions(xpv1.Condition{
			Type:    TypeUpstreamAuth,
			Status:  corev1.ConditionFalse,
			Reason:  ReasonUpstreamAuthRejected,
			Message: err.Error(),
		})
	case ClassNone:
		if clearOnSuccess {
			if c := status.GetCondition(TypeUpstreamAuth); c.Status == corev1.ConditionFalse {
				lg.V(1).Info("UpstreamAuth condition cleared",
					"previous_status", string(c.Status),
					"previous_message", c.Message)
				status.SetConditions(xpv1.Condition{
					Type:    TypeUpstreamAuth,
					Status:  corev1.ConditionTrue,
					Reason:  ReasonUpstreamAuthRecovering,
					Message: "Upstream Signoz API responds 2xx",
				})
			}
		}
	default:
		// transient / rate-limited — don't toggle UpstreamAuth; those
		// are surfaced via events and the breaker counter.
	}
}
