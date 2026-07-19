package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/muesli/termenv"
	log "github.com/sirupsen/logrus"
	ctrl "sigs.k8s.io/controller-runtime"

	"kuberstein.io/ingressh/internal/k8s"
	"kuberstein.io/ingressh/internal/types"
)

// Start runs the SSH ingress server until the context is cancelled.
func Start(ctx context.Context) error {
	conf := types.GetServerConf()

	kube := k8s.ClientImpl{}
	if err := kube.Init(ctrl.GetConfigOrDie()); err != nil {
		return fmt.Errorf("unable to create k8s client: %v", err)
	}

	srv, err := wish.NewServer(
		wish.WithAddress(conf.BindAddress),
		wish.WithHostKeyPath(conf.HostKeyFile),
		wish.WithPublicKeyAuth(PublicKeyAuthHandler),
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(DirectoryHandler(&kube, conf), termenv.ANSI256),
			SessionMiddleware(&kube, conf),
			logging.MiddlewareWithLogger(log.StandardLogger()),
		),
	)
	if err != nil {
		return fmt.Errorf("unable to create SSH server: %v", err)
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	log.Infof("Starting ssh ingress server at %s", conf.BindAddress)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		return err
	}
	return nil
}
