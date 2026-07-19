package server

import (
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"

	"kuberstein.io/ingressh/internal/k8s"
	"kuberstein.io/ingressh/internal/types"
)

// SessionMiddleware routes the sessions that need no interactive selection:
// sessions without a terminal and sessions whose login name hints the
// complete target are connected to the first authorized target right away.
// Everything else is passed through to the directory listing.
func SessionMiddleware(kube *k8s.ClientImpl, conf *types.ServerConfig) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sess ssh.Session) {
			// User may hint the target route with login name of SSH session.
			hint := types.SshTarget{}
			hint.InitFromUsername(sess.User())

			if _, _, isPty := sess.Pty(); isPty && !hint.IsComplete() {
				next(sess)
				return
			}

			client, endpoints, err := newDirectoryClient(sess, kube, conf, hint)
			if err != nil {
				wish.Fatalf(sess, "Error: %s\n", err)
				return
			}
			if len(endpoints) == 0 {
				wish.Fatalln(sess, "No authorized targets")
				return
			}
			if !hint.IsComplete() {
				wish.Printf(sess, "Hello %s, you will be connected to the first authorized target\n", sess.User())
			}
			if err := client.For(endpoints[0]).Run(); err != nil {
				wish.Fatalf(sess, "Error: %s\n", err)
			}
		}
	}
}
