package server

import (
	"context"
	"fmt"
	"os"

	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"

	ctrl "sigs.k8s.io/controller-runtime"

	"kuberstein.io/ingressh/internal/k8s"
	"kuberstein.io/ingressh/internal/types"
)

func Start(sshConfigPath string, ctx context.Context) error {
	conf, err := types.GetServerConf(sshConfigPath)
	if err != nil {
		return err
	}

	kube := k8s.ClientImpl{}
	if err := kube.Init(ctrl.GetConfigOrDie()); err != nil {
		return fmt.Errorf("unable to create k8s client: %v", err)
	}

	pemBytes, err := os.ReadFile(conf.HostKeyFile)
	if err != nil {
		return fmt.Errorf("unable to read host key file %s: %v", conf.HostKeyFile, err)
	}

	signer, err := gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		return fmt.Errorf("unable to parse private key: %v", err)
	}

	srv := &ssh.Server{
		Addr:             conf.BindAddress,
		PublicKeyHandler: PublicKeyAuthHandler,
		Handler:          GetHandler(&kube, conf),
		HostSigners:      []ssh.Signer{signer},
	}

	return srv.ListenAndServe()
}
