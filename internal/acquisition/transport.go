package acquisition

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
)

// Failure contains only classified, public acquisition details.
type Failure struct {
	message    string
	correction string
	routable   bool
}

func (e *Failure) Error() string      { return e.message }
func (e *Failure) Message() string    { return e.message }
func (e *Failure) Correction() string { return e.correction }
func (e *Failure) Routable() bool     { return e.routable }

// TransportFailure drops native error text while retaining destination refusals.
func TransportFailure(subject, endpoint string, err error) *Failure {
	var classified *Failure
	if errors.As(err, &classified) {
		return classified
	}

	host := "its approved publisher"
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	failure := &Failure{correction: "Restore this host's access to " + host, routable: true}
	condition := "could not be reached from this host"
	var resolution *net.DNSError
	var verification *tls.CertificateVerificationError
	var expired interface{ Timeout() bool }
	switch {
	case errors.As(err, &resolution):
		condition = "could not be resolved by this host's configured resolver"
		if resolution.IsTimeout {
			condition = "was not resolved before this host's resolver timed out"
		}
		failure.correction = "Make " + host + " resolvable"
	case errors.As(err, &verification), errors.As(err, new(x509.UnknownAuthorityError)),
		errors.As(err, new(x509.HostnameError)), errors.As(err, new(x509.CertificateInvalidError)):
		condition = "presented a certificate the qualified system trust store does not accept"
		failure.correction, failure.routable = "Install the required certificate authority in the system trust store", false
	case errors.As(err, &expired) && expired.Timeout():
		condition = "did not answer within its bounded acquisition timeout"
	}
	failure.message = subject + " " + host + " " + condition
	return failure
}
