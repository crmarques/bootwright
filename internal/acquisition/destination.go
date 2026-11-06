package acquisition

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type acquisitionEndpointKey struct{}
type acquisitionDialKey struct{}

type acquisitionDialPolicy struct {
	endpoint string
	private  bool
	literal  netip.Addr
}

// PrivateAcquisitionEndpoint grants the explicitly selected mirror access to
// private unicast addresses. Loopback/link-local require an explicit IP literal.
func PrivateAcquisitionEndpoint(ctx context.Context, endpoint *url.URL) context.Context {
	return context.WithValue(ctx, acquisitionEndpointKey{}, acquisitionAuthority(endpoint))
}

// AcquisitionBoundaryFailure is non-routable: another route cannot authorize
// an endpoint outside the declared acquisition boundary.
func AcquisitionBoundaryFailure(message string) error {
	return &Failure{message: message, correction: "Use the approved publisher or explicitly declare the intended mirror or proxy endpoint"}
}

// GuardAcquisitionTransport checks the actual connection target, including an
// explicit proxy, and pins the dial to a checked resolver answer.
func GuardAcquisitionTransport(base *http.Transport, approved func(*url.URL) bool) http.RoundTripper {
	guard := &acquisitionTransport{base: base, approved: approved, lookup: net.DefaultResolver.LookupNetIP, dial: base.DialContext}
	base.DialContext = guard.connect
	return guard
}

type acquisitionTransport struct {
	base     *http.Transport
	approved func(*url.URL) bool
	lookup   func(context.Context, string, string) ([]netip.Addr, error)
	dial     func(context.Context, string, string) (net.Conn, error)
}

func (g *acquisitionTransport) CloseIdleConnections() { g.base.CloseIdleConnections() }

func (g *acquisitionTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !g.approved(request.URL) {
		return nil, AcquisitionBoundaryFailure("dependency acquisition requested an unapproved endpoint")
	}
	endpoint := request.URL
	declared, _ := request.Context().Value(acquisitionEndpointKey{}).(string)
	private := declared != "" && declared == acquisitionAuthority(endpoint)
	if g.base.Proxy != nil {
		proxy, err := g.base.Proxy(request)
		if err != nil {
			return nil, err
		}
		if proxy != nil {
			endpoint, private = proxy, true
		}
	}
	port := endpoint.Port()
	if port == "" {
		port = "443"
		if endpoint.Scheme == "http" {
			port = "80"
		}
	}
	literal, _ := netip.ParseAddr(endpoint.Hostname())
	policy := acquisitionDialPolicy{endpoint: net.JoinHostPort(endpoint.Hostname(), port), private: private, literal: literal.Unmap()}
	return g.base.RoundTrip(request.WithContext(context.WithValue(request.Context(), acquisitionDialKey{}, policy)))
}

func (g *acquisitionTransport) connect(ctx context.Context, network, address string) (net.Conn, error) {
	policy, ok := ctx.Value(acquisitionDialKey{}).(acquisitionDialPolicy)
	if !ok || !strings.EqualFold(policy.endpoint, address) {
		return nil, AcquisitionBoundaryFailure("dependency acquisition attempted an unapproved connection endpoint")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, AcquisitionBoundaryFailure("dependency acquisition has an invalid connection endpoint")
	}
	addresses := []netip.Addr{policy.literal}
	if !policy.literal.IsValid() {
		addresses, err = g.lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(addresses) == 0 {
		return nil, &net.DNSError{Err: "no addresses", Name: host}
	}
	for _, address := range addresses {
		if !allowedAcquisitionAddress(address, policy) {
			return nil, AcquisitionBoundaryFailure("dependency acquisition resolved outside its approved address policy")
		}
	}
	var last error
	for _, address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		connection, err := g.dial(ctx, network, net.JoinHostPort(address.Unmap().String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	return nil, last
}

func allowedAcquisitionAddress(address netip.Addr, policy acquisitionDialPolicy) bool {
	address = address.Unmap()
	if !address.IsValid() || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	if address.IsLoopback() || address.IsLinkLocalUnicast() {
		return policy.private && policy.literal == address
	}
	if netip.MustParsePrefix("0.0.0.0/8").Contains(address) || netip.MustParsePrefix("240.0.0.0/4").Contains(address) || netip.MustParsePrefix("::/96").Contains(address) {
		return false
	}
	if !address.IsGlobalUnicast() {
		return false
	}
	if address.IsPrivate() {
		return policy.private
	}
	// Shared carrier space is not an Internet publisher destination.
	if netip.MustParsePrefix("100.64.0.0/10").Contains(address) {
		return policy.private
	}
	return true
}

func acquisitionAuthority(endpoint *url.URL) string {
	port := endpoint.Port()
	if port == "" {
		port = "443"
		if endpoint.Scheme == "http" {
			port = "80"
		}
	}
	return strings.ToLower(net.JoinHostPort(endpoint.Hostname(), port))
}
