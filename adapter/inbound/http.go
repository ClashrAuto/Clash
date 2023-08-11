package inbound

import (
	"net"

	C "github.com/ClashrAuto/clash/constant"
	"github.com/ClashrAuto/clash/context"
	"github.com/ClashrAuto/clash/transport/socks5"
)

// NewHTTP receive normal http request and return HTTPContext
func NewHTTP(target socks5.Addr, source net.Addr, conn net.Conn, additions ...Addition) *context.ConnContext {
	metadata := parseSocksAddr(target)
	metadata.NetWork = C.TCP
	metadata.Type = C.HTTP
	for _, addition := range additions {
		addition.Apply(metadata)
	}
	if ip, port, err := parseAddr(source); err == nil {
		metadata.SrcIP = ip
		metadata.SrcPort = port
	}
	if ip, port, err := parseAddr(conn.LocalAddr()); err == nil {
		metadata.InIP = ip
		metadata.InPort = port
	}
	return context.NewConnContext(conn, metadata)
}
