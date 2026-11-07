package nativeio

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type packetListenFunc func(context.Context, string, string) (net.PacketConn, error)

type udpTransport interface {
	io.Closer
	LocalAddr() net.Addr
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
	WriteToUDP([]byte, *net.UDPAddr) (int, error)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

type udpAddress struct {
	Network string `json:"network"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

func (a udpAddress) validate(bind bool) error {
	if a.Network != "udp" || a.Host == "" || strings.ContainsRune(a.Host, 0) {
		return errors.New("UDP requires network udp and a nonempty host without NUL")
	}
	minimum := 1
	if bind {
		minimum = 0
	}
	if a.Port < minimum || a.Port > 65535 {
		return errors.New("UDP port is out of range")
	}
	return nil
}

func dropPacketBind(value any) {
	if conn, ok := value.(udpTransport); ok {
		_ = conn.Close()
	}
}

func (h *Host) bindPacket(payload string, listen packetListenFunc) quickjs.Value {
	var address udpAddress
	if err := json.Unmarshal([]byte(payload), &address); err != nil {
		return h.rejectedNetOperation(err)
	}
	if err := address.validate(true); err != nil {
		return h.rejectedNetOperation(err)
	}
	if listen == nil {
		listen = (&net.ListenConfig{}).ListenPacket
	}
	return h.packetOperation(h.ctx, func(ctx context.Context) (any, error) {
		conn, err := listen(ctx, "udp", net.JoinHostPort(address.Host, strconv.Itoa(address.Port)))
		if err != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return nil, err
		}
		packet, ok := conn.(udpTransport)
		if !ok {
			_ = conn.Close()
			return nil, fmt.Errorf("unsupported UDP socket %T", conn)
		}
		return packet, nil
	}, func(value any) (any, io.Closer, error) {
		socket, err := newPacketSocket(h, value.(udpTransport))
		if err != nil {
			return nil, nil, err
		}
		object, err := socket.object()
		return object, socket, err
	}, dropPacketBind)
}

type packetSocket struct {
	h         *Host
	conn      udpTransport
	ctx       context.Context
	cancel    context.CancelFunc
	work      *quickjs.AsyncWork
	mu        sync.Mutex
	closed    bool
	io        sync.WaitGroup
	readGate  chan struct{}
	writeGate chan struct{}
	close     sync.Once
	closeErr  error
}

func newPacketSocket(h *Host, conn udpTransport) (*packetSocket, error) {
	ctx, cancel := context.WithCancel(h.ctx)
	s := &packetSocket{h: h, conn: conn, ctx: ctx, cancel: cancel, work: h.rt.StartAsyncWork(), readGate: make(chan struct{}, 1), writeGate: make(chan struct{}, 1)}
	s.readGate <- struct{}{}
	s.writeGate <- struct{}{}
	if !h.own(s) {
		return nil, os.ErrClosed
	}
	return s, nil
}

func (s *packetSocket) address(addr *net.UDPAddr) (quickjs.Value, error) {
	o := s.h.rt.NewObject()
	host := addr.IP.String()
	if addr.Zone != "" {
		host += "%" + addr.Zone
	}
	err := errors.Join(o.Set("network", "udp"), o.Set("host", host), o.Set("port", addr.Port))
	return o, err
}

func (s *packetSocket) object() (quickjs.Value, error) {
	addr, ok := s.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return quickjs.Value{}, errors.New("UDP socket has an invalid local address")
	}
	address, err := s.address(addr)
	if err != nil {
		return quickjs.Value{}, err
	}
	o := s.h.rt.NewObject()
	err = errors.Join(o.Set("address", address), o.Set("send", s.send), o.Set("receive", s.receive), o.Set("close", s.closePromise))
	return o, err
}

func (s *packetSocket) closePromise() *quickjs.Promise {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	return s.h.async(func() (any, error) { return nil, s.Close() }, nil, nil)
}

func (s *packetSocket) operation(run func(context.Context) (any, error), convert packetConvert) quickjs.Value {
	return s.h.packetOperation(s.ctx, func(ctx context.Context) (any, error) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, net.ErrClosed
		}
		s.io.Add(1)
		s.mu.Unlock()
		defer s.io.Done()
		return run(ctx)
	}, convert, nil)
}

// Only the active operation owns a descriptor deadline. Join its cancellation
// callback and clear the deadline before allowing the next operation to start.
func packetIO(ctx context.Context, gate chan struct{}, deadline func(time.Time) error, run func() (any, error)) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
	}
	defer func() { gate <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = deadline(time.Now())
		close(interrupted)
	})
	value, err := run()
	if !stop() {
		<-interrupted
	}
	resetErr := deadline(time.Time{})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return value, errors.Join(err, resetErr)
}

type udpDatagram struct {
	data []byte
	from *net.UDPAddr
}

func (s *packetSocket) receive() quickjs.Value {
	return s.operation(func(ctx context.Context) (any, error) {
		return packetIO(ctx, s.readGate, s.conn.SetReadDeadline, func() (any, error) {
			buffer := make([]byte, 65536)
			n, from, err := s.conn.ReadFromUDP(buffer)
			if err != nil {
				return nil, err
			}
			// Ordinary UDP payloads are smaller than this buffer. A full buffer
			// may be truncated (or a jumbo packet), so never publish its prefix.
			if n >= len(buffer) {
				return nil, &udpMessageSizeError{}
			}
			return udpDatagram{data: append([]byte{}, buffer[:n]...), from: from}, nil
		})
	}, func(value any) (any, io.Closer, error) {
		packet := value.(udpDatagram)
		address, err := s.address(packet.from)
		if err != nil {
			return nil, nil, err
		}
		o := s.h.rt.NewObject()
		err = errors.Join(o.Set("data", s.h.rt.NewBytes(packet.data)), o.Set("remoteAddress", address))
		return o, nil, err
	})
}

func (s *packetSocket) send(chunk quickjs.Value, payload string) quickjs.Value {
	bytes, ok := chunk.Bytes()
	if !ok {
		return s.h.rejectedNetOperation(errors.New("send requires Uint8Array"))
	}
	data := append([]byte{}, bytes...)
	var address udpAddress
	if err := json.Unmarshal([]byte(payload), &address); err != nil {
		return s.h.rejectedNetOperation(err)
	}
	if err := address.validate(false); err != nil {
		return s.h.rejectedNetOperation(err)
	}
	return s.operation(func(ctx context.Context) (any, error) {
		target, err := s.resolve(ctx, address)
		if err != nil {
			return nil, err
		}
		return packetIO(ctx, s.writeGate, s.conn.SetWriteDeadline, func() (any, error) {
			n, err := s.conn.WriteToUDP(data, target)
			if err == nil && n != len(data) {
				err = io.ErrShortWrite
			}
			return n, err
		})
	}, nil)
}

func (s *packetSocket) resolve(ctx context.Context, address udpAddress) (*net.UDPAddr, error) {
	if ip, err := netip.ParseAddr(address.Host); err == nil {
		return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, uint16(address.Port))), nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, address.Host)
	if err != nil {
		return nil, err
	}
	local, _ := s.conn.LocalAddr().(*net.UDPAddr)
	for _, ip := range ips {
		if local != nil && local.IP.To4() != nil && ip.IP.To4() == nil {
			continue
		}
		return &net.UDPAddr{IP: ip.IP, Zone: ip.Zone, Port: address.Port}, nil
	}
	return nil, &net.AddrError{Err: "no compatible IP address for UDP destination", Addr: address.Host}
}

func (s *packetSocket) Close() error {
	s.close.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		s.closeErr = s.conn.Close()
		s.io.Wait()
		s.h.release(s)
		s.work.Done()
	})
	if errors.Is(s.closeErr, net.ErrClosed) {
		return nil
	}
	return s.closeErr
}
