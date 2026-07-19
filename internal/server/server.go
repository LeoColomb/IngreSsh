package server

import (
	"context"
	"fmt"
	"net"
	"sync"

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

// Manager runs one SSH server per Gateway listener. The Gateway controller
// synchronizes the desired listeners; the manager starts and stops the
// corresponding wish servers.
type Manager struct {
	conf *types.ServerConfig
	kube *k8s.ClientImpl

	mu      sync.Mutex
	servers map[string]map[string]*managedServer
}

type managedServer struct {
	addr string
	srv  *ssh.Server
}

// NewManager creates a Manager with the server configuration taken from the
// environment and a Kubernetes client for the session handlers.
func NewManager() (*Manager, error) {
	kube := &k8s.ClientImpl{}
	if err := kube.Init(ctrl.GetConfigOrDie()); err != nil {
		return nil, fmt.Errorf("unable to create k8s client: %v", err)
	}
	return &Manager{
		conf:    types.GetServerConf(),
		kube:    kube,
		servers: map[string]map[string]*managedServer{},
	}, nil
}

// newServer assembles a wish SSH server listening at addr.
func (m *Manager) newServer(addr string) (*ssh.Server, error) {
	return wish.NewServer(
		wish.WithAddress(addr),
		wish.WithHostKeyPath(m.conf.HostKeyFile),
		wish.WithPublicKeyAuth(PublicKeyAuthHandler),
		wish.WithMiddleware(
			bubbletea.MiddlewareWithProgramHandler(DirectoryHandler(m.kube, m.conf), termenv.ANSI256),
			SessionMiddleware(m.kube, m.conf),
			logging.MiddlewareWithLogger(log.StandardLogger()),
		),
	)
}

// Sync reconciles the running SSH servers of the given Gateway with the
// desired listeners, a map of listener names to bind addresses: missing
// servers are started, changed ones are restarted, removed ones are stopped.
func (m *Manager) Sync(gateway string, listeners map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	running := m.servers[gateway]
	if running == nil {
		running = map[string]*managedServer{}
		m.servers[gateway] = running
	}

	for name, existing := range running {
		if addr, ok := listeners[name]; !ok || addr != existing.addr {
			log.Infof("Stopping ssh ingress server %s/%s at %s", gateway, name, existing.addr)
			_ = existing.srv.Close()
			delete(running, name)
		}
	}

	for name, addr := range listeners {
		if _, ok := running[name]; ok {
			continue
		}
		srv, err := m.newServer(addr)
		if err != nil {
			return fmt.Errorf("unable to create SSH server for %s/%s: %w", gateway, name, err)
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("unable to listen at %s for %s/%s: %w", addr, gateway, name, err)
		}
		log.Infof("Starting ssh ingress server %s/%s at %s", gateway, name, addr)
		go func() {
			if err := srv.Serve(ln); err != nil && err != ssh.ErrServerClosed {
				log.Errorf("SSH server %s/%s failed: %v", gateway, name, err)
			}
		}()
		running[name] = &managedServer{addr: addr, srv: srv}
	}

	if len(running) == 0 {
		delete(m.servers, gateway)
	}
	return nil
}

// Remove stops all the SSH servers of the given Gateway.
func (m *Manager) Remove(gateway string) {
	_ = m.Sync(gateway, nil)
}

// Start implements manager.Runnable so the controller manager owns the
// lifecycle: it blocks until the context is cancelled, then shuts down all
// the SSH servers.
func (m *Manager) Start(ctx context.Context) error {
	<-ctx.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	for gateway, listeners := range m.servers {
		for name, s := range listeners {
			log.Infof("Stopping ssh ingress server %s/%s at %s", gateway, name, s.addr)
			_ = s.srv.Close()
		}
	}
	m.servers = map[string]map[string]*managedServer{}
	return nil
}
