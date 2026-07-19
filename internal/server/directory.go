package server

import (
	"errors"
	"fmt"
	"io"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wishlist"
	log "github.com/sirupsen/logrus"

	"kuberstein.io/ingressh/internal/k8s"
	"kuberstein.io/ingressh/internal/types"
)

// target binds a pod and its SSH config with the selected container.
type target struct {
	podSshConfig
	container string
}

// directoryClient implements wishlist.SSHClient, connecting the endpoint
// picked in the directory listing to the corresponding container.
type directoryClient struct {
	sess    ssh.Session
	kube    *k8s.ClientImpl
	conf    *types.ServerConfig
	targets map[string]target
	program *tea.Program
}

// newDirectoryClient builds the wishlist endpoints for all the targets the
// session's user is authorized to access, applying the hint filtering.
// Endpoint names double as the login hint to connect to the target directly.
func newDirectoryClient(sess ssh.Session, kube *k8s.ClientImpl, conf *types.ServerConfig, hint types.SshTarget) (
	*directoryClient, []*wishlist.Endpoint, error,
) {
	targetAuth := GetAuthz(GetSshConfigsFromCtx(sess.Context()), kube)
	client := &directoryClient{
		sess:    sess,
		kube:    kube,
		conf:    conf,
		targets: map[string]target{},
	}

	endpoints := []*wishlist.Endpoint{}
	namespaces, err := targetAuth.GetNamespaces(hint.Namespace)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(namespaces)
	for _, namespace := range namespaces {
		podConfigs, err := targetAuth.GetPods(namespace, hint.Pod)
		if err != nil {
			return nil, nil, err
		}
		sort.Slice(podConfigs, func(i, j int) bool {
			return podConfigs[i].pod.Name < podConfigs[j].pod.Name
		})
		for _, podConfig := range podConfigs {
			containers, err := targetAuth.GetContainers(podConfig.pod, podConfig.config.Containers, hint.Container)
			if err != nil {
				return nil, nil, err
			}
			sort.Strings(containers)
			for _, container := range containers {
				name := fmt.Sprintf("%s:%s:%s", namespace, podConfig.pod.Name, container)
				client.targets[name] = target{podSshConfig: podConfig, container: container}
				endpoints = append(endpoints, &wishlist.Endpoint{
					Name:    name,
					Address: name,
					Desc:    wishlist.FirstNonEmpty(podConfig.config.Session, "Debug") + " session",
				})
			}
		}
	}

	return client, endpoints, nil
}

// For implements wishlist.SSHClient.
func (c *directoryClient) For(e *wishlist.Endpoint) tea.ExecCommand {
	return &connection{client: c, target: c.targets[e.Name]}
}

// connection implements tea.ExecCommand, attaching the SSH session to the
// target container while the directory listing is suspended.
type connection struct {
	client *directoryClient
	target target
}

// The SSH session is used directly for I/O, as it backs both the listing and
// the connection.
func (*connection) SetStdin(io.Reader)  {}
func (*connection) SetStdout(io.Writer) {}
func (*connection) SetStderr(io.Writer) {}

func (conn *connection) Run() error {
	sess, kube := conn.client.sess, conn.client.kube
	pod := conn.target.pod
	config := conn.target.config
	config.ApplyDefaults(*conn.client.conf)

	wish.Printf(sess, "Connecting your SSH session to %s/%s container %s...\n",
		pod.Namespace, pod.Name, conn.target.container)

	// Session attach options vary depending on the mode
	if config.Session == "Exec" {
		command := config.Command
		if len(sess.Command()) > 0 {
			command = sess.Command()
		}
		if len(command) == 0 {
			// In the Exec mode there is no default command to run like
			// in the Debug mode, where the docker image entry point
			// could be used.
			return errors.New("command is not specified")
		}
		log.Infof("Executing %v in the container %s", command, conn.target.container)
		if err := k8s.ExecInContainer(kube, &pod, conn.target.container, sess, command); err != nil {
			log.Errorln(err)
			return err
		}
	} else {
		// debug session mode
		debugPod, accessContainerName, err := k8s.AttachAccessContainer(kube, &pod, conn.target.container, config)
		if err != nil {
			log.Errorln(err)
			return err
		}

		if len(sess.Command()) > 0 {
			// Execute command in the running debug container
			log.Infof("Executing %v in the ephemeral container %s", sess.Command(), accessContainerName)
			err = k8s.ExecInContainer(kube, debugPod, accessContainerName, sess, sess.Command())
		} else {
			// Attach terminal session to the running debug container
			log.Infof("Attaching SSH session into the container %s", accessContainerName)
			err = k8s.AttachSshSessionTerminal(kube, debugPod, accessContainerName, sess)
		}
		if err != nil {
			log.Errorln(err)
			return err
		}
	}

	// The connection ended successfully: quit the listing to close the SSH
	// session. On errors the listing stays up, showing the failure and
	// letting the user pick another target.
	if conn.client.program != nil {
		conn.client.program.Quit()
	}
	return nil
}

// DirectoryHandler serves the interactive directory listing of the authorized
// targets, delegating the terminal UI entirely to wishlist.
func DirectoryHandler(kube *k8s.ClientImpl, conf *types.ServerConfig) bm.ProgramHandler {
	return func(sess ssh.Session) *tea.Program {
		hint := types.SshTarget{}
		hint.InitFromUsername(sess.User())

		client, endpoints, err := newDirectoryClient(sess, kube, conf, hint)
		if err != nil {
			wish.Fatalf(sess, "Error: %s\n", err)
			return nil
		}
		switch len(endpoints) {
		case 0:
			wish.Fatalln(sess, "No authorized targets")
			return nil
		case 1:
			// There is no choice to make, connect right away
			if err := client.For(endpoints[0]).Run(); err != nil {
				wish.Fatalf(sess, "Error: %s\n", err)
			}
			return nil
		}

		program := tea.NewProgram(
			wishlist.NewListing(endpoints, client),
			append(bm.MakeOptions(sess), tea.WithAltScreen())...,
		)
		client.program = program
		return program
	}
}
