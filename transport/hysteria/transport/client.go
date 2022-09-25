package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/ClashrAuto/clash/transport/hysteria/conns/faketcp"
	"github.com/ClashrAuto/clash/transport/hysteria/conns/udp"
	"github.com/ClashrAuto/clash/transport/hysteria/conns/wechat"
	"github.com/ClashrAuto/clash/transport/hysteria/obfs"
	"github.com/lucas-clemente/quic-go"
	"net"
)

type ClientTransport struct {
	Dialer *net.Dialer
}

func (ct *ClientTransport) quicPacketConn(proto string, server string, obfs obfs.Obfuscator, dialer PacketDialer) (net.PacketConn, error) {
	if len(proto) == 0 || proto == "udp" {
		conn, err := dialer.ListenPacket()
		if err != nil {
			return nil, err
		}
		if obfs != nil {
			oc := udp.NewObfsUDPConn(conn, obfs)
			return oc, nil
		} else {
			return conn, nil
		}
	} else if proto == "wechat-video" {
		conn, err := dialer.ListenPacket()
		if err != nil {
			return nil, err
		}
		if obfs != nil {
			oc := wechat.NewObfsWeChatUDPConn(conn, obfs)
			return oc, nil
		} else {
			return conn, nil
		}
	} else if proto == "faketcp" {
		var conn *faketcp.TCPConn
		conn, err := faketcp.Dial("tcp", server)
		if err != nil {
			return nil, err
		}
		if obfs != nil {
			oc := faketcp.NewObfsFakeTCPConn(conn, obfs)
			return oc, nil
		} else {
			return conn, nil
		}
	} else {
		return nil, fmt.Errorf("unsupported protocol: %s", proto)
	}
}

type PacketDialer interface {
	ListenPacket() (net.PacketConn, error)
	Context() context.Context
	RemoteAddr(host string) (net.Addr, error)
}

func (ct *ClientTransport) QUICDial(proto string, server string, tlsConfig *tls.Config, quicConfig *quic.Config, obfs obfs.Obfuscator, dialer PacketDialer) (quic.Connection, error) {
	serverUDPAddr, err := dialer.RemoteAddr(server)
	if err != nil {
		return nil, err
	}

	pktConn, err := ct.quicPacketConn(proto, serverUDPAddr.String(), obfs, dialer)
	if err != nil {
		return nil, err
	}

	qs, err := quic.DialContext(dialer.Context(), pktConn, serverUDPAddr, server, tlsConfig, quicConfig)
	if err != nil {
		_ = pktConn.Close()
		return nil, err
	}
	return qs, nil
}

func (ct *ClientTransport) DialTCP(raddr *net.TCPAddr) (*net.TCPConn, error) {
	conn, err := ct.Dialer.Dial("tcp", raddr.String())
	if err != nil {
		return nil, err
	}
	return conn.(*net.TCPConn), nil
}

func (ct *ClientTransport) ListenUDP() (*net.UDPConn, error) {
	return net.ListenUDP("udp", nil)
}