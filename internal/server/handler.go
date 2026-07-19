package server

import (
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	log "github.com/sirupsen/logrus"

	"kuberstein.io/ingressh/internal/k8s"
	"kuberstein.io/ingressh/internal/types"
)

// SessionMiddleware returns the SSH connection middleware for the SSH server.
// The user is authorized at this moment, the list of authorized configurations
// is stored in the session context.
func SessionMiddleware(kube *k8s.ClientImpl, conf *types.ServerConfig) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			// User may hint the target route with login name of SSH session.
			hint := types.SshTarget{}
			hint.InitFromUsername(sess.User())

			targetAuth := GetAuthz(GetSshConfigsFromCtx(sess.Context()), kube)

			var target types.SshTarget
			var targetPodConfig podSshConfig
			var err error
			_, _, isPty := sess.Pty()

			// Interactive selection makes sense only when there is a terminal
			// and the user didn't specify all the components of the target
			// to connect to.
			if isPty && !hint.IsComplete() {
				target, targetPodConfig, err = interactive(sess, targetAuth, hint)
			} else {
				target, targetPodConfig, err = automatic(sess, targetAuth, hint)
			}
			if err != nil {
				wish.Fatalf(sess, "Error: %s\n", err)
				return
			}
			if !target.IsComplete() {
				wish.Fatalln(sess, "No container selected")
				return
			}

			targetConfig := targetPodConfig.config
			targetConfig.ApplyDefaults(*conf)
			pod := targetPodConfig.pod

			wish.Printf(sess, "Pod has been found. Connecting your SSH session to %s/%s container %s...\n",
				pod.Namespace, pod.Name, target.Container)

			// Session attach options vary depending on the mode
			if targetConfig.Session == "Exec" {
				command := targetConfig.Command
				if len(sess.Command()) > 0 {
					command = sess.Command()
				}
				if len(command) == 0 {
					// In the Exec mode there is no default command to run like
					// in the Debug mode, where the docker image entry point
					// could be used.
					wish.Fatalln(sess, "Command is not specified")
					return
				}
				log.Infof("Executing %v in the container %s", command, target.Container)
				if err := k8s.ExecInContainer(kube, &pod, target.Container, sess, command); err != nil {
					log.Errorln(err)
					wish.Fatalln(sess, "Failed to execute the command")
					return
				}
			} else {
				// debug session mode
				pod, accessContainerName, err := k8s.AttachAccessContainer(
					kube, &pod, target.Container, targetConfig)
				if err != nil {
					log.Errorln(err)
					wish.Fatalln(sess, "Failed to attach the access container")
					return
				}

				if len(sess.Command()) > 0 {
					// Execute command in the running debug container
					log.Infof("Executing %v in the ephemeral container %s", sess.Command(), accessContainerName)
					err = k8s.ExecInContainer(kube, pod, accessContainerName, sess, sess.Command())
				} else {
					// Attach terminal session to the running debug container
					log.Infof("Attaching SSH session into the container %s", accessContainerName)
					err = k8s.AttachSshSessionTerminal(kube, pod, accessContainerName, sess)
				}
				if err != nil {
					log.Errorln(err)
					wish.Fatalln(sess, "Failed to set up the session")
					return
				}
			}

			next(sess)
		}
	}
}
