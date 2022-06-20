package outboundgroup

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ClashrAuto/clash/adapter/outbound"
	"github.com/ClashrAuto/clash/common/singledo"
	"github.com/ClashrAuto/clash/component/dialer"
	C "github.com/ClashrAuto/clash/constant"
	"github.com/ClashrAuto/clash/constant/provider"
)

type urlTestOption func(*URLTest)

func urlTestWithTolerance(tolerance uint16) urlTestOption {
	return func(u *URLTest) {
		u.tolerance = tolerance
	}
}

type URLTest struct {
	*GroupBase
	tolerance  uint16
	disableUDP bool
	fastNode   C.Proxy
	fastSingle *singledo.Single[C.Proxy]
}

func (u *URLTest) Now() string {
	return u.fast(false).Name()
}

// DialContext implements C.ProxyAdapter
func (u *URLTest) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (c C.Conn, err error) {
	c, err = u.fast(true).DialContext(ctx, metadata, u.Base.DialOptions(opts...)...)
	if err == nil {
		c.AppendToChains(u)
		u.onDialSuccess()
	} else {
		u.onDialFailed()
	}
	return c, err
}

// ListenPacketContext implements C.ProxyAdapter
func (u *URLTest) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	pc, err := u.fast(true).ListenPacketContext(ctx, metadata, u.Base.DialOptions(opts...)...)
	if err == nil {
		pc.AppendToChains(u)
	}

	return pc, err
}

// Unwrap implements C.ProxyAdapter
func (u *URLTest) Unwrap(*C.Metadata) C.Proxy {
	return u.fast(true)
}

func (u *URLTest) fast(touch bool) C.Proxy {
	elm, _, _ := u.fastSingle.Do(func() (C.Proxy, error) {
		proxies := u.GetProxies(touch)

		// 检测所有代理是否有下载速度测试
		var proxyFromSpeed []C.Proxy
		for _, p := range proxies {

			if p.LastSpeed() > 0 {
				proxyFromSpeed = append(proxyFromSpeed, p)
			}
		}

		fast := proxies[0]
		min := fast.LastDelay()
		max := fast.LastSpeed()
		fastNotExist := true

		for _, proxy := range proxies[1:] {
			if u.fastNode != nil && proxy.Name() == u.fastNode.Name() {
				fastNotExist = false
			}

			if !proxy.Alive() {
				continue
			}

			speed := proxy.LastSpeed()
			if speed > max {
				fast = proxy
				max = speed
			}

			delay := proxy.LastDelay()
			if delay < min {
				fast = proxy
				min = delay
			}
		}

		if max > 0 {
			// tolerance
			if u.fastNode == nil || fastNotExist || !u.fastNode.Alive() || u.fastNode.LastSpeed() < fast.LastSpeed()-float64(u.tolerance) {
				u.fastNode = fast
			}
		} else {
			// tolerance
			if u.fastNode == nil || fastNotExist || !u.fastNode.Alive() || u.fastNode.LastDelay() > fast.LastDelay()+u.tolerance {
				u.fastNode = fast
			}
		}

		return u.fastNode, nil
	})

	return elm
}

// SupportUDP implements C.ProxyAdapter
func (u *URLTest) SupportUDP() bool {
	if u.disableUDP {
		return false
	}

	return u.fast(false).SupportUDP()
}

// MarshalJSON implements C.ProxyAdapter
func (u *URLTest) MarshalJSON() ([]byte, error) {
	all := []string{}
	for _, proxy := range u.GetProxies(false) {
		all = append(all, proxy.Name())
	}
	return json.Marshal(map[string]any{
		"type": u.Type().String(),
		"now":  u.Now(),
		"all":  all,
	})
}

func parseURLTestOption(config map[string]any) []urlTestOption {
	opts := []urlTestOption{}

	// tolerance
	if elm, ok := config["tolerance"]; ok {
		if tolerance, ok := elm.(int); ok {
			opts = append(opts, urlTestWithTolerance(uint16(tolerance)))
		}
	}

	return opts
}

func NewURLTest(option *GroupCommonOption, providers []provider.ProxyProvider, options ...urlTestOption) *URLTest {
	urlTest := &URLTest{
		GroupBase: NewGroupBase(GroupBaseOption{
			outbound.BaseOption{
				Name:        option.Name,
				Type:        C.URLTest,
				Interface:   option.Interface,
				RoutingMark: option.RoutingMark,
			},

			option.Filter,
			providers,
		}),
		fastSingle: singledo.NewSingle[C.Proxy](time.Second * 10),
		disableUDP: option.DisableUDP,
	}

	for _, option := range options {
		option(urlTest)
	}

	return urlTest
}
