package nativeio

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"time"
)

// SocketTLSOptions configures raw TLS without granting RawSockets. Referenced
// certificates, pools and callbacks retain Go's immutable-in-use ownership.
type SocketTLSOptions struct {
	Client, Server *tls.Config
	// Zero selects ten seconds; a negative value disables the handshake timer.
	HandshakeTimeout time.Duration
}

func CloneSocketTLS(options *SocketTLSOptions) *SocketTLSOptions {
	if options == nil {
		return nil
	}
	copy := *options
	if copy.Client != nil {
		copy.Client = copy.Client.Clone()
	}
	if copy.Server != nil {
		copy.Server = copy.Server.Clone()
	}
	return &copy
}

type netTLSOptionError struct{ message string }

func (e *netTLSOptionError) Error() string { return e.message }

func (h *Host) socketTLS(request netConnectRequest, server bool) (*tls.Config, error) {
	if request.Network != "tls" {
		return nil, nil
	}
	var configured *tls.Config
	if options := h.opts.RawSocketsTLS; options != nil {
		configured = options.Client
		if server {
			configured = options.Server
		}
	}
	inline := request.Cert != "" || request.Key != "" || request.CA != "" || request.ServerName != ""
	if configured != nil && inline {
		return nil, &netTLSOptionError{"inline TLS options cannot override host-configured TLS"}
	}
	config := &tls.Config{}
	if configured != nil {
		config = configured.Clone()
	} else {
		if request.Cert != "" || request.Key != "" {
			pair, err := tls.X509KeyPair([]byte(request.Cert), []byte(request.Key))
			if err != nil {
				return nil, &netTLSOptionError{"invalid TLS certificate or private key PEM pair"}
			}
			config.Certificates = []tls.Certificate{pair}
		}
		if request.CA != "" {
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM([]byte(request.CA)) {
				return nil, &netTLSOptionError{"invalid TLS CA certificate PEM"}
			}
			config.RootCAs = roots
		}
		config.ServerName = request.ServerName
	}
	if server {
		if len(config.Certificates) == 0 && config.GetCertificate == nil && config.GetConfigForClient == nil {
			return nil, &netTLSOptionError{"TLS listeners require a certificate/key PEM pair or host certificate configuration"}
		}
	} else if config.ServerName == "" {
		config.ServerName = request.Host
	}
	return config, nil
}

func (h *Host) handshakeContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := 10 * time.Second
	if options := h.opts.RawSocketsTLS; options != nil && options.HandshakeTimeout != 0 {
		timeout = options.HandshakeTimeout
	}
	if timeout < 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func (h *Host) tlsDial(dial netDialFunc, config *tls.Config) netDialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		transport, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		secure := tls.Client(transport, config)
		handshakeCtx, cancel := h.handshakeContext(ctx)
		defer cancel()
		if err := secure.HandshakeContext(handshakeCtx); err != nil {
			_ = transport.Close()
			return nil, err
		}
		return &tlsSocketConn{Conn: secure}, nil
	}
}

func (l *netListener) acceptConnection() (net.Conn, error) {
	transport, err := l.bound.Accept()
	if err != nil || l.bound.tlsConfig == nil {
		return transport, err
	}
	secure := &tlsSocketConn{Conn: tls.Server(transport, l.bound.tlsConfig)}
	ctx, cancel := l.h.handshakeContext(l.h.ctx)
	defer cancel()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = transport.Close()
		return nil, net.ErrClosed
	}
	if l.pending == nil {
		l.pending = make(map[net.Conn]context.CancelFunc)
	}
	l.pending[secure] = cancel
	l.mu.Unlock()
	if err := secure.HandshakeContext(ctx); err != nil {
		_ = transport.Close()
		l.mu.Lock()
		delete(l.pending, secure)
		l.mu.Unlock()
		return nil, err
	}
	return secure, nil
}

// TLS has close_notify for writes but no read-only shutdown alert. Local
// cancellation interrupts reads with a deadline; full close skips graceful
// alert writes so runtime teardown never waits behind TLS's writer mutex.
type tlsSocketConn struct{ *tls.Conn }

func (c *tlsSocketConn) CloseRead() error { return c.SetReadDeadline(time.Now()) }
func (c *tlsSocketConn) Close() error     { return c.NetConn().Close() }
func (c *tlsSocketConn) abortWrite() error {
	if transport, ok := c.NetConn().(interface{ CloseWrite() error }); ok {
		return transport.CloseWrite()
	}
	return c.SetWriteDeadline(time.Now())
}
