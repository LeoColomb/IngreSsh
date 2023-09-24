# SSH ingress for Kubernetes

The project implements a Kubernetes ingress controller, which routes incoming
SSH connections to the shell sessions at authorized pods. Authorization and
routing are configured as IngreSsh Kubernetes resources.

## Description

"How can I SSH into the running pod in Kubernetes?" is probably the first
question a new software developer asks a Kubernetes administrator. The usual
answer is "You can't, but there is kubectl exec/kubectl cp which are doing
the same."

Kubectl does the trick indeed, but it looks like people just have a warm fuzzy
feeling about connecting with the familiar SSH to any environment like
Linux. As there are no roadblocks to implementing this scenario with the
Kubernetes model and available SSH libraries, the project provides the
implementation of an SSH ingress controller for Kubernetes. The controller
can route incoming SSH connections to shell sessions started in the
context of the target pods.

* This might be useful for users not comfortable with kubectl or who have no
  kubectl configured/installed
* The user could access the container for the application's debug purposes
  without the API server is exposed outside the secured perimeter
* It is possible to configure a predefined debug image with all the required
  tools to be used for shell sessions.This allows the administrator to control
  what is running as debug containers without allowing users to run whatever
  they want or set up a special security policies

Incoming SSH connections are authenticated with the authorized keys, configured
in the ingress resource parameters. Ingress resource also contains
authorization rules, limiting which pods or containers the user can
access.

A shell is opened either as an exec command in the target container or as an
attach session to the debug container started automatically upon incoming
connection in the Linux namespace of the target container.

The project is implemented with:

* kubebuilder
* GliderLabs SSH libraries
* CharmBracelet libraries

## Demo

Connecting to the pod in the cluster using SSH ingress:

[![asciicast](https://asciinema.org/a/gh6CTevs3p55ARhVcKLYNPizF.svg)](https://asciinema.org/a/gh6CTevs3p55ARhVcKLYNPizF)

## Configuration

### IngreSsh Resource

Scheme of ingress resource.

### Server Configuration

The server configuration consists of server's RSA private key (k8s Secret),
and configuration file (k8s ConfigMap)

## How to try it from the source

You’ll need a Kubernetes cluster to run against. You can use
[KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run
against a remote cluster.

**Note:** Your controller will automatically use the current context in your
kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

If you are going to use `kind` for experiments, then the following should be
enough:

```sh
kind create cluster
```

Install CRD:

```sh
kubectl apply -f manifests/k8s/crd.yaml
```

Run some pods:

```sh
kubectl apply -f manifests/samples/nginx.yaml
```

Then put your authorized key as an element in the spec.authorizedKeys list
for sample IngreSsh resources in `manifests/samples/ingressh-exec.yaml` and
create the resource:

```sh
kubectl apply -f manifests/samples/ingressh-exec.yaml
```

Build and run the controller. This will run in the foreground, so switch to a
new terminal if you want to leave it running:

```sh
make run
```

In another console window run SSH to connect to the pod:

```sh
ssh 127.0.0.1 -p 2222
```

## Run with the docker image

After installing CRD, creating some pods, and modifying IngreSsh resource
putting your authorized key (see the previous section),
build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/ingressh:tag
```

Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/ingressh:tag
```

To UnDeploy the controller from the cluster:

```sh
make undeploy
```

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)