package drivers

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHTunnel forwards local TCP connections through an SSH server to a remote target.
type SSHTunnel struct {
	localAddr  string
	sshClient  *ssh.Client
	listener   net.Listener
	targetHost string
	targetPort int

	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

// LocalAddr returns the local address the tunnel is listening on.
func (t *SSHTunnel) LocalAddr() string {
	return t.localAddr
}

// Stop shuts down the tunnel, closing the listener, SSH client, and all forwarded connections.
func (t *SSHTunnel) Stop() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	if t.sshClient != nil {
		t.sshClient.Close()
	}
	t.wg.Wait()
}

// StartSSHTunnel establishes an SSH connection and starts a local TCP forwarder.
// Returns the tunnel and the local address to use as the database host:port.
func StartSSHTunnel(cfg DriverConfig) (*SSHTunnel, error) {
	if cfg.SSHHost == "" || cfg.SSHUsername == "" {
		return nil, fmt.Errorf("SSH host and username are required")
	}

	sshPort := cfg.SSHPort
	if sshPort == 0 {
		sshPort = 22
	}

	sshAddr := fmt.Sprintf("%s:%d", cfg.SSHHost, sshPort)

	sshConfig := &ssh.ClientConfig{
		User:            cfg.SSHUsername,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if cfg.SSHKey != "" {
		signer, err := parseSSHKey(cfg.SSHKey, cfg.SSHKeyPassphrase)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH key: %w", err)
		}
		sshConfig.Auth = append(sshConfig.Auth, ssh.PublicKeys(signer))
	}

	if cfg.SSHPassword != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.Password(cfg.SSHPassword))
	}

	if len(sshConfig.Auth) == 0 {
		return nil, fmt.Errorf("SSH password or private key is required")
	}

	client, err := ssh.Dial("tcp", sshAddr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("SSH dial to %s failed: %w", sshAddr, err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to start local listener: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	tunnel := &SSHTunnel{
		localAddr:  listener.Addr().String(),
		sshClient:  client,
		listener:   listener,
		targetHost: cfg.Host,
		targetPort: cfg.Port,
		cancel:     cancel,
	}

	tunnel.wg.Add(1)
	go tunnel.acceptLoop(ctx)

	return tunnel, nil
}

func (t *SSHTunnel) acceptLoop(ctx context.Context) {
	defer t.wg.Done()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}

		t.wg.Add(1)
		go t.forward(ctx, conn)
	}
}

func (t *SSHTunnel) forward(ctx context.Context, localConn net.Conn) {
	defer t.wg.Done()
	defer localConn.Close()

	target := fmt.Sprintf("%s:%d", t.targetHost, t.targetPort)
	remoteConn, err := t.sshClient.Dial("tcp", target)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(remoteConn, localConn)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(localConn, remoteConn)
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

func parseSSHKey(pemKey, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase([]byte(pemKey), []byte(passphrase))
	}
	return ssh.ParsePrivateKey([]byte(pemKey))
}

// TunnelManager manages shared SSH tunnels keyed by data source ID.
// Multiple connections to the same data source share a single tunnel.
type TunnelManager struct {
	mu      sync.Mutex
	tunnels map[string]*tunnelEntry
}

type tunnelEntry struct {
	tunnel *SSHTunnel
	refs   int
}

var tunnelMgr = &TunnelManager{
	tunnels: make(map[string]*tunnelEntry),
}

// TunnelMgr returns the global tunnel manager.
func TunnelMgr() *TunnelManager {
	return tunnelMgr
}

// AcquireTunnel returns an existing tunnel for the given data source or creates a new one.
// Each call must be paired with a ReleaseTunnel call.
func (m *TunnelManager) AcquireTunnel(dsID string, cfg DriverConfig) (*SSHTunnel, error) {
	m.mu.Lock()
	if entry, ok := m.tunnels[dsID]; ok {
		entry.refs++
		m.mu.Unlock()
		return entry.tunnel, nil
	}
	m.mu.Unlock()

	tunnel, err := StartSSHTunnel(cfg)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if entry, ok := m.tunnels[dsID]; ok {
		m.mu.Unlock()
		tunnel.Stop()
		entry.refs++
		return entry.tunnel, nil
	}
	m.tunnels[dsID] = &tunnelEntry{tunnel: tunnel, refs: 1}
	m.mu.Unlock()

	return tunnel, nil
}

// ReleaseTunnel decrements the reference count. When it reaches zero, the tunnel is stopped.
func (m *TunnelManager) ReleaseTunnel(dsID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.tunnels[dsID]
	if !ok {
		return
	}
	entry.refs--
	if entry.refs <= 0 {
		entry.tunnel.Stop()
		delete(m.tunnels, dsID)
	}
}

// StopAll shuts down all active tunnels.
func (m *TunnelManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, entry := range m.tunnels {
		entry.tunnel.Stop()
		delete(m.tunnels, id)
	}
}
