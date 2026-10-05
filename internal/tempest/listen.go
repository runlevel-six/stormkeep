package tempest

import (
	"context"
	"errors"
	"net"
	"syscall"
)

// Listen receives datagrams on addr (normally ":50222") until ctx is done,
// passing each decoded message to handle. Undecodable datagrams go to bad, if
// it is not nil; ignored message types go nowhere.
//
// The socket is opened with SO_REUSEADDR so it can share the port with another
// listener on the same host, such as weewx's WeatherFlow driver with
// share_socket enabled. Broadcast datagrams are delivered to every socket
// bound to the port, so neither listener takes anything from the other.
//
// Broadcasts reach only sockets on the network they were sent on. In a
// container that means the host's network namespace, not a pod network.
func Listen(ctx context.Context, addr string, handle func(any), bad func(error)) error {
	lc := net.ListenConfig{Control: reuseAddr}
	conn, err := lc.ListenPacket(ctx, "udp4", addr)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		msg, err := Parse(buf[:n])
		switch {
		case err == nil:
			handle(msg)
		case errors.Is(err, ErrIgnored):
		case bad != nil:
			bad(err)
		}
	}
}

func reuseAddr(_, _ string, c syscall.RawConn) error {
	var serr error
	err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
