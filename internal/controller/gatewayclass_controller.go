package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// GatewayControllerName identifies this controller in GatewayClass resources.
const GatewayControllerName gatewayv1.GatewayController = "kuberstein.io/ingressh"

// GatewayClassReconciler accepts the GatewayClasses claiming this controller.
type GatewayClassReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update;patch

// Reconcile marks the GatewayClasses referencing this controller as Accepted.
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	gatewayClass := &gatewayv1.GatewayClass{}
	if err := r.Get(ctx, req.NamespacedName, gatewayClass); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if gatewayClass.Spec.ControllerName != GatewayControllerName {
		return ctrl.Result{}, nil
	}

	changed := meta.SetStatusCondition(&gatewayClass.Status.Conditions, metav1.Condition{
		Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayClassReasonAccepted),
		Message:            "Accepted by the IngreSsh controller",
		ObservedGeneration: gatewayClass.Generation,
	})
	if !changed {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Update(ctx, gatewayClass)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.GatewayClass{}).
		Complete(r)
}
