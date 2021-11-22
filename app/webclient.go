package app

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"
)

var portMap = map[string]string{
	"http":  "80",
	"https": "443",
}

func (webClient *webClient) resolve(host string) (*net.IPAddr, error) {
	return net.ResolveIPAddr(webClient.config.IpProtocol(), host)
}

type webClient struct {
	httpClient *http.Client
	reused     bool
	ipAddr     *net.IPAddr
	config     Config
	url        *url.URL
	dialCtx    func(ctx context.Context, network, addr string) (net.Conn, error)
}

func (webClient *webClient) resetHttpClient() {

	webClient.httpClient = &http.Client{
		Timeout: webClient.config.Wait(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	webClient.httpClient.Transport = &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           webClient.dialCtx,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewWebClient builds a new instance of webClient which will provides functions for Http-Ping
func NewWebClient(config Config) (*webClient, error) {

	webClient := webClient{config: config}
	webClient.url, _ = url.Parse(config.Target())

	ipAddr, err := webClient.resolve(webClient.url.Hostname())

	if err != nil {
		return nil, err
	}

	webClient.ipAddr = ipAddr

	dialer := &net.Dialer{}

	webClient.dialCtx = func(ctx context.Context, network, addr string) (net.Conn, error) {

		var port = webClient.url.Port()
		if port == "" {
			port = portMap[webClient.url.Scheme]
		}

		return dialer.DialContext(ctx, network, fmt.Sprintf("[%s]:%s", ipAddr.IP.String(), port))
	}

	webClient.resetHttpClient()

	return &webClient, nil
}

func (webClient *webClient) DoConnection() error {
	timeOut := webClient.httpClient.Timeout
	webClient.httpClient.Timeout = 5 * time.Second
	_, err := webClient.DoMeasure()
	webClient.httpClient.Timeout = timeOut
	return err
}

func (webClient *webClient) DoMeasure() (*Answer, error) {
	closing := func() {
		if !webClient.config.KeepAlive() {
			webClient.resetHttpClient()
		}
	}

	defer closing()

	req, _ := http.NewRequest(webClient.config.Method(), webClient.config.Target(), nil)

	clientTrace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			webClient.reused = info.Reused
		},
	}
	traceCtx := httptrace.WithClientTrace(context.Background(), clientTrace)

	req = req.WithContext(traceCtx)

	start := time.Now()

	req.Header.Set("User-Agent", webClient.config.UserAgent())

	res, err := webClient.httpClient.Do(req)

	if err != nil {
		return nil, err
	}
	s, err := io.Copy(ioutil.Discard, res.Body)
	if err != nil {
		return nil, err
	}

	_ = res.Body.Close()
	var d = time.Since(start)

	return &Answer{
		Duration:     d,
		StatusCode:   res.StatusCode,
		Bytes:        s,
		SocketReused: webClient.reused,
	}, nil

}
