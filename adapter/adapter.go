package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/Dreamacro/clash/common/atomic"
	"github.com/Dreamacro/clash/common/queue"
	"github.com/Dreamacro/clash/common/utils"
	"github.com/Dreamacro/clash/component/dialer"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/log"

	"github.com/VividCortex/ewma"
)

var UnifiedDelay = atomic.NewBool(false)

const (
	defaultHistoriesNum = 10
)

type extraProxyState struct {
	history *queue.Queue[C.DelayHistory]
	alive   *atomic.Bool
}

type Proxy struct {
	C.ProxyAdapter
	history *queue.Queue[C.DelayHistory]
	alive   *atomic.Bool
	url     string
	extra   map[string]*extraProxyState
}

// Alive implements C.Proxy
func (p *Proxy) Alive() bool {
	return p.alive.Load()
}

// AliveForTestUrl implements C.Proxy
func (p *Proxy) AliveForTestUrl(url string) bool {
	if p.extra != nil {
		if state, ok := p.extra[url]; ok {
			return state.alive.Load()
		}
	}

	return p.alive.Load()
}

// Dial implements C.Proxy
func (p *Proxy) Dial(metadata *C.Metadata) (C.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), C.DefaultTCPTimeout)
	defer cancel()
	return p.DialContext(ctx, metadata)
}

// DialContext implements C.ProxyAdapter
func (p *Proxy) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.Conn, error) {
	conn, err := p.ProxyAdapter.DialContext(ctx, metadata, opts...)
	return conn, err
}

// DialUDP implements C.ProxyAdapter
func (p *Proxy) DialUDP(metadata *C.Metadata) (C.PacketConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), C.DefaultUDPTimeout)
	defer cancel()
	return p.ListenPacketContext(ctx, metadata)
}

// ListenPacketContext implements C.ProxyAdapter
func (p *Proxy) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	pc, err := p.ProxyAdapter.ListenPacketContext(ctx, metadata, opts...)
	return pc, err
}

// DelayHistory implements C.Proxy
func (p *Proxy) DelayHistory() []C.DelayHistory {
	queueM := p.history.Copy()
	histories := []C.DelayHistory{}
	for _, item := range queueM {
		histories = append(histories, item)
	}
	return histories
}

// PutHistory implements C.Proxy
func (p *Proxy) PutHistory(his []C.DelayHistory) {
	for _, h := range his {
		p.history.Put(h)
	}
}

// DelayHistoryForTestUrl implements C.Proxy
func (p *Proxy) DelayHistoryForTestUrl(url string) []C.DelayHistory {
	var queueM []C.DelayHistory
	if p.extra != nil {
		if state, ok := p.extra[url]; ok {
			queueM = state.history.Copy()
		}
	}

	if queueM == nil {
		queueM = p.history.Copy()
	}

	histories := []C.DelayHistory{}
	for _, item := range queueM {
		histories = append(histories, item)
	}
	return histories
}

func (p *Proxy) ExtraDelayHistory() map[string][]C.DelayHistory {
	extra := map[string][]C.DelayHistory{}
	if p.extra != nil && len(p.extra) != 0 {
		for testUrl, option := range p.extra {
			histories := []C.DelayHistory{}
			queueM := option.history.Copy()
			for _, item := range queueM {
				histories = append(histories, item)
			}

			extra[testUrl] = histories
		}
	}
	return extra
}

// LastDelay return last history record. if proxy is not alive, return the max value of uint16.
// implements C.Proxy
func (p *Proxy) LastDelay() (delay uint16) {
	var max uint16 = 0xffff
	if !p.alive.Load() {
		return max
	}

	history := p.history.Last()
	if history.Delay == 0 {
		return max
	}
	return history.Delay
}

// LastDelay return last history record. if proxy is not alive, return the max value of uint16.
// implements C.Proxy
func (p *Proxy) LastSpeed() (speed float64) {
	var max float64 = 0
	if !p.alive.Load() {
		return max
	}

	history := p.history.Last()
	if history.Speed == 0 {
		return max
	}
	return history.Speed
}

// LastDelayForTestUrl implements C.Proxy
func (p *Proxy) LastDelayForTestUrl(url string) (delay uint16) {
	var max uint16 = 0xffff

	alive := p.alive.Load()
	history := p.history.Last()

	if p.extra != nil {
		if state, ok := p.extra[url]; ok {
			alive = state.alive.Load()
			history = state.history.Last()
		}
	}

	if !alive {
		return max
	}

	if history.Delay == 0 {
		return max
	}
	return history.Delay
}

// MarshalJSON implements C.ProxyAdapter
func (p *Proxy) MarshalJSON() ([]byte, error) {
	inner, err := p.ProxyAdapter.MarshalJSON()
	if err != nil {
		return inner, err
	}

	mapping := map[string]any{}
	_ = json.Unmarshal(inner, &mapping)
	mapping["history"] = p.DelayHistory()
	mapping["extra"] = p.ExtraDelayHistory()
	mapping["name"] = p.Name()
	mapping["udp"] = p.SupportUDP()
	mapping["xudp"] = p.SupportXUDP()
	mapping["tfo"] = p.SupportTFO()
	return json.Marshal(mapping)
}

// URLTest get the delay for the specified URL
// implements C.Proxy
func (p *Proxy) URLTest(ctx context.Context, url string, expectedStatus utils.IntRanges[uint16], store C.DelayHistoryStoreType) (t uint16, err error) {
	defer func() {
		alive := err == nil
		store = p.determineFinalStoreType(store, url)

		switch store {
		case C.OriginalHistory:
			p.alive.Store(alive)
			record := C.DelayHistory{Time: time.Now()}
			if alive {
				record.Delay = t
				record.Speed = p.history.Last().Speed
			}
			p.history.Put(record)
			if p.history.Len() > defaultHistoriesNum {
				p.history.Pop()
			}

			// test URL configured by the proxy provider
			if len(p.url) == 0 {
				p.url = url
			}
		case C.ExtraHistory:
			record := C.DelayHistory{Time: time.Now()}
			if alive {
				record.Delay = t
				record.Speed = p.history.Last().Speed
			}

			if p.extra == nil {
				p.extra = map[string]*extraProxyState{}
			}

			state, ok := p.extra[url]
			if !ok {
				state = &extraProxyState{
					history: queue.New[C.DelayHistory](defaultHistoriesNum),
					alive:   atomic.NewBool(true),
				}
				p.extra[url] = state
			}

			state.alive.Store(alive)
			state.history.Put(record)
			if state.history.Len() > defaultHistoriesNum {
				state.history.Pop()
			}
		default:
			log.Debugln("health check result will be discarded, url: %s alive: %t, delay: %d", url, alive, t)
		}
	}()

	unifiedDelay := UnifiedDelay.Load()

	addr, err := urlToMetadata(url)
	if err != nil {
		return
	}

	start := time.Now()
	instance, err := p.DialContext(ctx, &addr)
	if err != nil {
		return
	}
	defer func() {
		_ = instance.Close()
	}()

	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return
	}
	req = req.WithContext(ctx)

	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return instance, nil
		},
		// from http.DefaultTransport
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	defer client.CloseIdleConnections()

	resp, err := client.Do(req)

	if err != nil {
		return
	}

	_ = resp.Body.Close()

	if unifiedDelay {
		second := time.Now()
		resp, err = client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			start = second
		}
	}

	if expectedStatus != nil && !expectedStatus.Check(uint16(resp.StatusCode)) {
		// maybe another value should be returned for differentiation
		err = errors.New("response status is inconsistent with the expected status")
	}

	t = uint16(time.Since(start) / time.Millisecond)
	return
}

func (p *Proxy) URLDownload(timeout int, url string) (t float64, err error) {
	defer func() {
		p.alive.Store(err == nil)
		record := C.DelayHistory{Time: time.Now()}
		if err == nil {
			record.Speed = t
			record.Delay = p.history.Last().Delay
		}
		p.history.Put(record)
		if p.history.Len() > 10 {
			p.history.Pop()
		}
	}()

	addr, err := urlToMetadata(url)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*time.Duration(timeout))
	defer cancel()

	instance, err := p.DialContext(ctx, &addr)
	if err != nil {
		return
	}
	defer func() {
		_ = instance.Close()
	}()

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req = req.WithContext(ctx)

	transport := &http.Transport{
		Dial: func(string, string) (net.Conn, error) {
			return instance, nil
		},
		// from http.DefaultTransport
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)

	if err != nil {
		t = 0
	} else {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == 200 {

			var downloadTestTime = time.Millisecond * time.Duration(timeout)

			timeStart := time.Now()
			timeEnd := timeStart.Add(downloadTestTime)

			contentLength := resp.ContentLength
			buffer := make([]byte, contentLength)

			var contentRead int64 = 0
			var timeSlice = downloadTestTime / 100
			var timeCounter = 1
			var lastContentRead int64 = 0

			var nextTime = timeStart.Add(timeSlice * time.Duration(timeCounter))
			e := ewma.NewMovingAverage()

			for contentLength != contentRead {
				var currentTime = time.Now()
				if currentTime.After(nextTime) {
					timeCounter += 1
					nextTime = timeStart.Add(timeSlice * time.Duration(timeCounter))
					e.Add(float64(contentRead - lastContentRead))
					lastContentRead = contentRead
				}
				if currentTime.After(timeEnd) {
					break
				}
				bufferRead, err := resp.Body.Read(buffer)
				contentRead += int64(bufferRead)
				if err != nil {
					if err != io.EOF {
						break
					} else {
						e.Add(float64(contentRead-lastContentRead) / (float64(nextTime.Sub(currentTime)) / float64(timeSlice)))
					}
				}
			}
			t = e.Value() / (downloadTestTime.Seconds() / 100)
		} else {
			t = 0
		}
	}
	return
}

func NewProxy(adapter C.ProxyAdapter) *Proxy {
	return &Proxy{adapter, queue.New[C.DelayHistory](defaultHistoriesNum), atomic.NewBool(true), "", map[string]*extraProxyState{}}
}

func urlToMetadata(rawURL string) (addr C.Metadata, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return
	}

	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			err = fmt.Errorf("%s scheme not Support", rawURL)
			return
		}
	}

	addr = C.Metadata{
		Host:    u.Hostname(),
		DstIP:   netip.Addr{},
		DstPort: port,
	}
	return
}

func (p *Proxy) determineFinalStoreType(store C.DelayHistoryStoreType, url string) C.DelayHistoryStoreType {
	if store != C.DropHistory {
		return store
	}

	if len(p.url) == 0 || url == p.url {
		return C.OriginalHistory
	}

	if p.extra == nil {
		store = C.ExtraHistory
	} else {
		if _, ok := p.extra[url]; ok {
			store = C.ExtraHistory
		} else if len(p.extra) < 2*C.DefaultMaxHealthCheckUrlNum {
			store = C.ExtraHistory
		}
	}
	return store
}
