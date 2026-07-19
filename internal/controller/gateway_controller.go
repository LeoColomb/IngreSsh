package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gw "kuberstein.io/ingressh/api/v1alpha1"
	"kuberstein.io/ingressh/internal/server"
)

// SSHProtocolType is the custom Gateway listener protocol served by this
// controller, next to the core TCP protocol.
const SSHProtocolType gatewayv1.ProtocolType = "kuberstein.io/ssh"

// GatewayReconciler runs an SSH server for every listener of the Gateways
// belonging to a GatewayClass of this controller.
type GatewayReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Servers *server.Manager
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways/status,verbs=get;update;patch

// Reconcile synchronizes the SSH servers with the Gateway listeners and
// reports the result in the Gateway status.
func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	gateway := &gatewayv1.Gateway{}
	if err := r.Get(ctx, req.NamespacedName, gateway); err != nil {
		// The servers of a deleted Gateway are stopped; nothing else to
		// clean up, so no finalizer is needed.
		r.Servers.Remove(req.String())
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if owns, err := r.ownsGateway(ctx, gateway); err != nil || !owns {
		return ctrl.Result{}, err
	}

	if !gateway.DeletionTimestamp.IsZero() {
		r.Servers.Remove(req.String())
		return ctrl.Result{}, nil
	}

	routes, err := r.attachedRoutes(ctx, req.NamespacedName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Collect the SSH listeners and their per-listener status.
	listeners := map[string]string{}
	statuses := []gatewayv1.ListenerStatus{}
	for _, listener := range gateway.Spec.Listeners {
		accepted := listener.Protocol == gatewayv1.TCPProtocolType || listener.Protocol == SSHProtocolType
		if accepted {
			listeners[string(listener.Name)] = fmt.Sprintf(":%d", listener.Port)
		}
		statuses = append(statuses, listenerStatus(listener, accepted, routes[string(listener.Name)], gateway.Generation))
	}

	syncErr := r.Servers.Sync(req.String(), listeners)

	meta.SetStatusCondition(&gateway.Status.Conditions, metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonAccepted),
		Message:            "Accepted by the IngreSsh controller",
		ObservedGeneration: gateway.Generation,
	})
	programmed := metav1.Condition{
		Type:               string(gatewayv1.GatewayConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.GatewayReasonProgrammed),
		Message:            "SSH servers are listening",
		ObservedGeneration: gateway.Generation,
	}
	if syncErr != nil {
		programmed.Status = metav1.ConditionFalse
		programmed.Reason = string(gatewayv1.GatewayReasonListenersNotValid)
		programmed.Message = syncErr.Error()
	}
	meta.SetStatusCondition(&gateway.Status.Conditions, programmed)
	gateway.Status.Listeners = statuses

	if err := r.Status().Update(ctx, gateway); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, syncErr
}

// ownsGateway tells whether the Gateway belongs to a GatewayClass claiming
// this controller.
func (r *GatewayReconciler) ownsGateway(ctx context.Context, gateway *gatewayv1.Gateway) (bool, error) {
	gatewayClass := &gatewayv1.GatewayClass{}
	err := r.Get(ctx, ktypes.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, gatewayClass)
	if err != nil {
		return false, client.IgnoreNotFound(err)
	}
	return gatewayClass.Spec.ControllerName == GatewayControllerName, nil
}

// attachedRoutes counts the SSHRoutes attached to each listener of the
// Gateway. Routes that do not pick a listener with sectionName count for
// every listener.
func (r *GatewayReconciler) attachedRoutes(ctx context.Context, gateway ktypes.NamespacedName) (map[string]int32, error) {
	routes := &gw.SSHRouteList{}
	if err := r.List(ctx, routes); err != nil {
		return nil, err
	}
	counts := map[string]int32{}
	for _, route := range routes.Items {
		for _, ref := range route.Spec.ParentRefs {
			if !refersToGateway(route.Namespace, ref, gateway) {
				continue
			}
			counts[sectionName(ref)]++
		}
	}
	return counts, nil
}

func sectionName(ref gatewayv1.ParentReference) string {
	if ref.SectionName == nil {
		return ""
	}
	return string(*ref.SectionName)
}

// listenerStatus builds the status of a single Gateway listener.
func listenerStatus(listener gatewayv1.Listener, accepted bool, attached int32, generation int64) gatewayv1.ListenerStatus {
	condition := metav1.Condition{
		Type:               string(gatewayv1.ListenerConditionAccepted),
		Status:             metav1.ConditionTrue,
		Reason:             string(gatewayv1.ListenerReasonAccepted),
		Message:            "SSH listener accepted",
		ObservedGeneration: generation,
	}
	if !accepted {
		condition.Status = metav1.ConditionFalse
		condition.Reason = string(gatewayv1.ListenerReasonUnsupportedProtocol)
		condition.Message = fmt.Sprintf("Supported protocols are %s and %s", gatewayv1.TCPProtocolType, SSHProtocolType)
	}
	status := gatewayv1.ListenerStatus{
		Name: listener.Name,
		SupportedKinds: []gatewayv1.RouteGroupKind{{
			Group: (*gatewayv1.Group)(&gw.GroupVersion.Group),
			Kind:  "SSHRoute",
		}},
		AttachedRoutes: attached,
	}
	meta.SetStatusCondition(&status.Conditions, condition)
	return status
}

// refersToGateway tells whether a parentRef of a route in routeNamespace
// references the given Gateway.
func refersToGateway(routeNamespace string, ref gatewayv1.ParentReference, gateway ktypes.NamespacedName) bool {
	if ref.Group != nil && *ref.Group != gatewayv1.GroupName {
		return false
	}
	if ref.Kind != nil && *ref.Kind != "Gateway" {
		return false
	}
	namespace := routeNamespace
	if ref.Namespace != nil {
		namespace = string(*ref.Namespace)
	}
	return namespace == gateway.Namespace && string(ref.Name) == gateway.Name
}

// SetupWithManager sets up the controller with the Manager. Gateways are
// also reconciled when attached SSHRoutes change, to refresh the route
// counters of the listeners.
func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Watches(&gw.SSHRoute{}, handler.EnqueueRequestsFromMapFunc(routeParents)).
		Complete(r)
}

// routeParents maps an SSHRoute event to the Gateways it attaches to.
func routeParents(_ context.Context, obj client.Object) []reconcile.Request {
	route, ok := obj.(*gw.SSHRoute)
	if !ok {
		return nil
	}
	requests := []reconcile.Request{}
	for _, ref := range route.Spec.ParentRefs {
		namespace := route.Namespace
		if ref.Namespace != nil {
			namespace = string(*ref.Namespace)
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: ktypes.NamespacedName{Namespace: namespace, Name: string(ref.Name)},
		})
	}
	return requests
}
