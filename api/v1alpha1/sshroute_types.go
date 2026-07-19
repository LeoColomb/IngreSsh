package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// AuthorizedKey is a structure joining user's login name and public key.
// The login name is used for audit/logs and not influence login
// or authorization parameters. It also is independent of what user
// specifies as a login part of the connection sting as something@cluster.
// Users are only matched with their public keys.
type AuthorizedKey struct {
	// User specifies the login name of the user.
	// It is used only for audit.
	// +optional
	User string `json:"user,omitempty"`

	// Key is a public key to authorize login
	// The keys are specified in the same format as lines in the
	// .ssh/authorized_keys file
	Key string `json:"key"`
}

// SSHRouteSpec defines the desired state of SSHRoute.
// An SSHRoute configures access to pods through the SSH sessions served by
// the Gateways (gateway.networking.k8s.io) it attaches to via parentRefs.
// Users, authorized with their public keys, can establish SSH connection
// with the pods accordingly to the configured pods selectors.
// SSHRoute resources are namespace-scoped.
type SSHRouteSpec struct {
	gatewayv1.CommonRouteSpec `json:",inline"`

	// Session specifies the mechanism to use for the SSH session of this
	// route: exec in container (Exec) or ephemeral container (Debug)
	// Debug is the default.
	// +kubebuilder:validation:Enum=Debug;Exec
	// +optional
	Session string `json:"session,omitempty"`

	// Image for the ephemeral container. If not specified the default from the
	// server configuration is used. The option is relevant for the Debug
	// type sessions. For the Exec type sessions it has no effect.
	// +optional
	Image string `json:"image,omitempty"`

	// A command to execute as the login shell for the SSH session. This will
	// run in interactive mode when the user executes `ssh cluster` command.
	//
	// For the Debug session mode it sets entrypoint array for the docker
	// image of the ephermeral container. See the description of the
	// corresponding field in the ephemeral container spec
	// (https://github.com/kubernetes/api/blob/master/core/v1/types.go)
	// If not specified, an entrypoint of the docker image of the ephemeral
	// container will be used.
	//
	// For the Exec session mode functions like a login shell for the user.
	//
	// If the user specifies command as a part of the ssh connect string (f.e.
	// `ssh cluster ls -l`), the specified command will be used instead of the
	// login shell in the Exec session mode. For the Debug session mode an
	// ephemeral container will be started with the entrypoint defined in
	// this configuration, and then the specified command will be used in
	// scope of the SSH session.
	//
	// Please note that SSH does not set up terminal when running the command
	// specified via command line. If the user runs `ssh cluster /bin/bash`
	// there will be no normal terminal support. It is OK for non-interactive
	// commands like `ssh cluster ls -l`
	//
	// This means that although in theory you may not specify the command here,
	// in practice you would like to set it up to allow interactive sessions
	// in the Exec session mode.
	//
	// +optional
	Command []string `json:"command,omitempty"`

	// Arguments to the entrypoint.
	// The image's CMD is used if this is not provided.
	// See the description of corresponding field in the ephemeral container
	// spec (https://github.com/kubernetes/api/blob/master/core/v1/types.go)
	// +optional
	Args []string `json:"args,omitempty"`

	// Container's working directory to drop SSH session to.
	// If not specified, the container runtime's default will be used, which
	// might be configured in the container image.
	// +optional
	WorkingDir string `json:"workingDir,omitempty"`

	// Selectors define target pods to authorize SSH session to.
	// If not specified, all pods could be accessed by the authorized user.
	// A user can specify one of the authorized pods as the login part
	// of SSH connection string, like `ssh pod-name@cluster /bin/bash`
	// As SSHRoute resources are namespace-scoped, selectors are matched
	// against pods in the resource's namespace.
	// +optional
	Selectors []string `json:"selectors,omitempty"`

	// If specified, containers define the list of container names to attach
	// SSH session to. The first container in the target pod, which matches one
	// of the container names in the list, will be attached. If the target pod
	// contains none of the specified container names session can not be
	// created.
	//
	// If not specified, all containers can be attached.
	//
	// A user can specify the container to attach as part of the login
	// part of the the SSH connection command, like
	// `ssh namespace:pod:container@cluster` where the namespace and pod parts
	// can be omitted: `ssh ::container@cluster`
	//
	// +optional
	Containers []string `json:"containers,omitempty"`

	// AuthorizedKeys is a set of public keys to authorize login
	// The keys are specified in the same format as lines in the
	// .ssh/authorized_keys file
	//
	// +kubebuilder:validation:MinItems=1
	AuthorizedKeys []AuthorizedKey `json:"authorizedKeys"`
}

// SSHRouteStatus defines the observed state of SSHRoute
type SSHRouteStatus struct {
	gatewayv1.RouteStatus `json:",inline"`

	// Information when was the last time the ssh session was opened.
	// +optional
	LastlogTime *metav1.Time `json:"lastlogTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SSHRoute is the Schema for the sshroutes API
type SSHRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SSHRouteSpec   `json:"spec,omitempty"`
	Status SSHRouteStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SSHRouteList contains a list of SSHRoute
type SSHRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SSHRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SSHRoute{}, &SSHRouteList{})
}
