package runthrough

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
)

func validateUpstreamEndpoint(cfg UpstreamConfig) error {
	if cfg.Endpoint == "" {
		return nil
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return errors.New("upstream endpoint requires an absolute HTTP(S) URL without userinfo or fragments")
	}
	if endpoint.Scheme == "https" {
		return nil
	}
	if endpoint.Scheme != "http" {
		return errors.New("upstream endpoint requires HTTPS")
	}
	ip := net.ParseIP(endpoint.Hostname())
	if cfg.AllowInsecureHTTP || ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("upstream HTTP is restricted to literal loopback addresses; explicitly enable insecure upstream HTTP for other hosts")
}

func confinedUpstreamClient() *http.Client {
	return &http.Client{Transport: awshttp.NewBuildableClient().GetTransport(), CheckRedirect: confinedUpstreamRedirect}
}

func confinedUpstreamRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 || len(via) >= 10 {
		return errors.New("upstream redirect limit reached")
	}
	initial := via[0].URL
	if request.URL.Scheme != initial.Scheme || !strings.EqualFold(request.URL.Host, initial.Host) || request.URL.User != nil {
		return errors.New("upstream redirect to a different origin refused")
	}
	if request.Response == nil || request.Response.StatusCode != http.StatusTemporaryRedirect && request.Response.StatusCode != http.StatusPermanentRedirect {
		return http.ErrUseLastResponse
	}
	return nil
}
