package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	tlsC "github.com/ClashrAuto/clash/component/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/ClashrAuto/clash/component/dialer"
	C "github.com/ClashrAuto/clash/constant"
)

type Http struct {
	*Base
	user      string
	pass      string
	tlsConfig *tls.Config
	option    *HttpOption
}

type HttpOption struct {
	BasicOption
	Name           string            `proxy:"name"`
	Server         string            `proxy:"server"`
	Port           int               `proxy:"port"`
	UserName       string            `proxy:"username,omitempty"`
	Password       string            `proxy:"password,omitempty"`
	TLS            bool              `proxy:"tls,omitempty"`
	SNI            string            `proxy:"sni,omitempty"`
	SkipCertVerify bool              `proxy:"skip-cert-verify,omitempty"`
	Fingerprint    string            `proxy:"fingerprint,omitempty"`
	Headers        map[string]string `proxy:"headers,omitempty"`
}

// StreamConn implements C.ProxyAdapter
func (h *Http) StreamConn(c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	if h.tlsConfig != nil {
		cc := tls.Client(c, h.tlsConfig)
		err := cc.Handshake()
		c = cc
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", h.addr, err)
		}
	}

	if err := h.shakeHand(metadata, c); err != nil {
		return nil, err
	}
	return c, nil
}

// DialContext implements C.ProxyAdapter
func (h *Http) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.Conn, err error) {
	c, err := dialer.DialContext(ctx, "tcp", h.addr, h.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", h.addr, err)
	}
	tcpKeepAlive(c)

	defer safeConnClose(c, err)

	c, err = h.StreamConn(c, metadata)
	if err != nil {
		return nil, err
	}

	return NewConn(c, h), nil
}

func (h *Http) shakeHand(metadata *C.Metadata, rw io.ReadWriter) error {
	addr := metadata.RemoteAddress()
	req := &http.Request{
		Method: http.MethodConnect,
		URL: &url.URL{
			Host: addr,
		},
		Host: addr,
		Header: http.Header{
			"Proxy-Connection": []string{"Keep-Alive"},
		},
	}

	//增加headers
	if len(h.option.Headers) != 0 {
		for key, value := range h.option.Headers {
			req.Header.Add(key, value)
		}
	}

	if h.user != "" && h.pass != "" {
		auth := h.user + ":" + h.pass
		req.Header.Add("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}

	if err := req.Write(rw); err != nil {
		return err
	}

	resp, err := http.ReadResponse(bufio.NewReader(rw), req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	if resp.StatusCode == http.StatusProxyAuthRequired {
		return errors.New("HTTP need auth")
	}

	if resp.StatusCode == http.StatusMethodNotAllowed {
		return errors.New("CONNECT method not allowed by proxy")
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		return errors.New(resp.Status)
	}

	return fmt.Errorf("can not connect remote err code: %d", resp.StatusCode)
}

func NewHttp(option HttpOption) (*Http, error) {
	var tlsConfig *tls.Config
	if option.TLS {
		sni := option.Server
		if option.SNI != "" {
			sni = option.SNI
		}
		if len(option.Fingerprint) == 0 {
			tlsConfig = tlsC.GetGlobalFingerprintTLCConfig(&tls.Config{
				InsecureSkipVerify: option.SkipCertVerify,
				ServerName:         sni,
			})
		} else {
			var err error
			if tlsConfig, err = tlsC.GetSpecifiedFingerprintTLSConfig(&tls.Config{
				InsecureSkipVerify: option.SkipCertVerify,
				ServerName:         sni,
			}, option.Fingerprint); err != nil {
				return nil, err
			}
		}
	}

	return &Http{
		Base: &Base{
			name:   option.Name,
			addr:   net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			tp:     C.Http,
			iface:  option.Interface,
			rmark:  option.RoutingMark,
			prefer: C.NewDNSPrefer(option.IPVersion),
		},
		user:      option.UserName,
		pass:      option.Password,
		tlsConfig: tlsConfig,
		option:    &option,
	}, nil
}
