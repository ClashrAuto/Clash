//go:build no_gvisor

package gvisor

import (
	"fmt"
	"github.com/ClashrAuto/clash/adapter/inbound"
	C "github.com/ClashrAuto/clash/constant"
	"github.com/ClashrAuto/clash/listener/tun/device"
	"github.com/ClashrAuto/clash/listener/tun/ipstack"
	"github.com/ClashrAuto/clash/log"
	"net/netip"
)

// New allocates a new *gvStack with given options.
func New(device device.Device, dnsHijack []netip.AddrPort, tunAddress netip.Prefix, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) (ipstack.Stack, error) {
	log.Fatalln("unsupported gvisor stack on the build")
	return nil, fmt.Errorf("unsupported gvisor stack on the build")
}
