package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gw "kuberstein.io/ingressh/api/v1alpha1"
	"kuberstein.io/ingressh/internal/server"
	"kuberstein.io/ingressh/internal/types"
)

// SSHRouteReconciler reconciles an SSHRoute object into the routing table of
// the SSH servers.
type SSHRouteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// finalizerName is the name of the custom finalizer to handle the resource deletion
const finalizerName = "gateway.kuberstein.io/finalizer"

//+kubebuilder:rbac:groups=gateway.kuberstein.io,resources=sshroutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.kuberstein.io,resources=sshroutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway.kuberstein.io,resources=sshroutes/finalizers,verbs=update

// Reconcile programs the routes of an SSHRoute resource into the SSH server
// routing table when the route is accepted by at least one Gateway of this
// controller, and reports the acceptance in the route status.
func (r *SSHRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	route := &gw.SSHRoute{}
	if err := r.Get(ctx, req.NamespacedName, route); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Init ssh configuration object from the route object
	sshConfig := &types.SshConfig{
		SSHRouteSpec: route.Spec,
		Name:         req.Name,
		Namespace:    req.Namespace,
	}

	if !route.DeletionTimestamp.IsZero() {
		// The object is being deleted
		log.Info("Delete routes from the SSH server accordingly to resource configuration")
		if controllerutil.ContainsFinalizer(route, finalizerName) {
			server.Routes.Delete(sshConfig)
			controllerutil.RemoveFinalizer(route, finalizerName)
			if err := r.Update(ctx, route); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// The object is not being deleted. Register finalizer to handle
	// the future deletion.
	if !controllerutil.ContainsFinalizer(route, finalizerName) {
		controllerutil.AddFinalizer(route, finalizerName)
		if err := r.Update(ctx, route); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve the parents; the route is programmed if at least one Gateway
	// of this controller accepts it.
	parents := []gatewayv1.RouteParentStatus{}
	accepted := false
	for _, ref := range route.Spec.ParentRefs {
		condition, err := r.acceptedByParent(ctx, route, ref)
		if err != nil {
			return ctrl.Result{}, err
		}
		if condition == nil {
			// The parent is not managed by this controller
			continue
		}
		condition.ObservedGeneration = route.Generation
		accepted = accepted || condition.Status == metav1.ConditionTrue
		parents = append(parents, gatewayv1.RouteParentStatus{
			ParentRef:      ref,
			ControllerName: GatewayControllerName,
			Conditions:     []metav1.Condition{*condition},
		})
	}

	if accepted {
		log.Info("Update configuration of SSH server")
		server.Routes.Set(sshConfig)
	} else {
		log.Info("No accepting Gateway, removing the routes from the SSH server")
		server.Routes.Delete(sshConfig)
	}

	route.Status.Parents = parents
	return ctrl.Result{}, r.Status().Update(ctx, route)
}

// acceptedByParent resolves a single parentRef. It returns nil if the
// referenced parent is not a Gateway managed by this controller, and the
// Accepted condition to report otherwise.
func (r *SSHRouteReconciler) acceptedByParent(ctx context.Context, route *gw.SSHRoute, ref gatewayv1.ParentReference) (*metav1.Condition, error) {
	if (ref.Group != nil && *ref.Group != gatewayv1.GroupName) || (ref.Kind != nil && *ref.Kind != "Gateway") {
		return nil, nil
	}

	namespace := route.Namespace
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}

	gateway := &gatewayv1.Gateway{}
	err := r.Get(ctx, ktypes.NamespacedName{Namespace: namespace, Name: string(ref.Name)}, gateway)
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if err != nil {
		return &metav1.Condition{
			Type:    string(gatewayv1.RouteConditionAccepted),
			Status:  metav1.ConditionFalse,
			Reason:  string(gatewayv1.RouteReasonNoMatchingParent),
			Message: "Gateway not found",
		}, nil
	}

	gatewayClass := &gatewayv1.GatewayClass{}
	err = r.Get(ctx, ktypes.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, gatewayClass)
	if client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	if err != nil || gatewayClass.Spec.ControllerName != GatewayControllerName {
		return nil, nil
	}

	return &metav1.Condition{
		Type:    string(gatewayv1.RouteConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayv1.RouteReasonAccepted),
		Message: "Route accepted by the IngreSsh controller",
	}, nil
}

// SetupWithManager sets up the controller with the Manager. Routes are also
// reconciled when Gateways change, as the acceptance depends on the parents.
func (r *SSHRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gw.SSHRoute{}).
		Watches(&gatewayv1.Gateway{}, handler.EnqueueRequestsFromMapFunc(r.gatewayRoutes)).
		Complete(r)
}

// gatewayRoutes maps a Gateway event to the SSHRoutes attached to it.
func (r *SSHRouteReconciler) gatewayRoutes(ctx context.Context, obj client.Object) []reconcile.Request {
	routes := &gw.SSHRouteList{}
	if err := r.List(ctx, routes); err != nil {
		return nil
	}
	gateway := ktypes.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	requests := []reconcile.Request{}
	for _, route := range routes.Items {
		for _, ref := range route.Spec.ParentRefs {
			if refersToGateway(route.Namespace, ref, gateway) {
				requests = append(requests, reconcile.Request{
					NamespacedName: ktypes.NamespacedName{Namespace: route.Namespace, Name: route.Name},
				})
				break
			}
		}
	}
	return requests
}
