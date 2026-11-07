package stdlib

import (
	"crypto/tls"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// listenerTLS snapshots all JS input and validates before authorization or
// binding. PEM is data, so TLS installation never reads a host filesystem.
func (s *servers) listenerTLS(opts quickjs.Value) (*tls.Config, error) {
	var cert, key string
	var supplied bool
	if opts.Kind() == quickjs.KindObject {
		certValue, err := opts.Get("cert")
		if err != nil {
			return nil, err
		}
		keyValue, err := opts.Get("key")
		if err != nil {
			return nil, err
		}
		supplied = !certValue.IsUndefined() || !keyValue.IsUndefined()
		if supplied {
			if certValue.Kind() != quickjs.KindString || keyValue.Kind() != quickjs.KindString {
				return nil, s.rt.Throw(s.rt.NewError("TypeError", "HTTPS requires both cert and key as PEM strings"))
			}
			cert, key = certValue.String(), keyValue.String()
			if cert == "" || key == "" {
				return nil, s.rt.Throw(s.rt.NewError("TypeError", "HTTPS requires nonempty cert and key PEM strings"))
			}
		}
	}
	if supplied && s.cfg.TLS != nil {
		return nil, s.rt.Throw(s.rt.NewError("TypeError", "cert and key cannot override host-configured TLS"))
	}
	config := s.cfg.TLS
	if supplied {
		pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
		if err != nil {
			// Keep both the certificate and private key out of diagnostics.
			return nil, s.rt.Throw(s.rt.NewError("TypeError", "invalid HTTPS certificate or private key PEM pair"))
		}
		config = &tls.Config{Certificates: []tls.Certificate{pair}}
	}
	if config == nil {
		return nil, nil
	}
	if len(config.Certificates) == 0 && config.GetCertificate == nil && config.GetConfigForClient == nil {
		return nil, s.rt.Throw(s.rt.NewError("TypeError", "host HTTPS requires a certificate or certificate callback"))
	}
	return listenerTLSConfig(config), nil
}

// The shared HTTP and WebSocket backend speaks HTTP/1.1. Normalize a private
// copy, including callback-selected configurations, without mutating host TLS
// policy or certificate ownership. TLS callbacks run on Go's connection work.
func listenerTLSConfig(config *tls.Config) *tls.Config {
	owned := config.Clone()
	owned.NextProtos = []string{"http/1.1"}
	if selectConfig := config.GetConfigForClient; selectConfig != nil {
		owned.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			selected, err := selectConfig(hello)
			if err != nil || selected == nil {
				return selected, err
			}
			result := selected.Clone()
			result.NextProtos = []string{"http/1.1"}
			return result, nil
		}
	}
	return owned
}
