package bundlelocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func singleDiagnostic(t *testing.T, err error) desiredstate.Diagnostic {
	t.Helper()
	diagnostics := desiredstate.DiagnosticsOf(err)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %d from %v", len(diagnostics), err)
	}
	return diagnostics[0]
}

func TestUnreachablePublisherNamesItsHostAndCorrectableCondition(t *testing.T) {
	endpoint := "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"
	for _, test := range []struct {
		name      string
		cause     error
		condition string
		next      string
	}{
		{"resolver timeout", &net.DNSError{Err: "i/o timeout", Name: "raw.githubusercontent.com", IsTimeout: true}, "resolver timed out", "resolvable before setup"},
		{"unknown name", &net.DNSError{Err: "no such host", Name: "raw.githubusercontent.com", IsNotFound: true}, "could not be resolved", "resolvable before setup"},
		{"untrusted certificate", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "qualified system trust store does not accept", "certificate authority in the system trust store"},
		{"unknown authority", x509.UnknownAuthorityError{}, "qualified system trust store does not accept", "certificate authority in the system trust store"},
		{"response timeout", context.DeadlineExceeded, "bounded acquisition timeout", "Restore this host's access"},
		{"refused", errors.New("connect: connection refused"), "could not be reached from this host", "Restore this host's access"},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := singleDiagnostic(t, transportFailure("bootstrap publisher", endpoint, &url.Error{Op: "Get", URL: endpoint, Err: test.cause}))
			if diagnostic.Code != "controller.setup" || !strings.Contains(diagnostic.Message, "bootstrap publisher raw.githubusercontent.com") || !strings.Contains(diagnostic.Message, test.condition) {
				t.Fatalf("message did not classify the failing publisher: %q", diagnostic.Message)
			}
			if !strings.Contains(diagnostic.Remediation, test.next) {
				t.Fatalf("remediation is not the action this condition needs: %q", diagnostic.Remediation)
			}
			if strings.Contains(diagnostic.Message+diagnostic.Remediation, "/astral-sh/") || strings.Contains(diagnostic.Message+diagnostic.Remediation, test.cause.Error()) {
				t.Fatalf("diagnostic leaked the request path or transport text: %q %q", diagnostic.Message, diagnostic.Remediation)
			}
		})
	}
}

func TestClassifiedAcquisitionRefusalKeepsItsOwnDiagnostic(t *testing.T) {
	boundary := bundleFailure("dependency acquisition redirected outside its approved HTTPS boundary")
	wrapped := transportFailure("bootstrap publisher", "https://pypi.org/pypi/ansible-core/json", &url.Error{Op: "Get", URL: "https://pypi.org", Err: boundary})
	if singleDiagnostic(t, wrapped).Message != singleDiagnostic(t, boundary).Message {
		t.Fatal("an already classified refusal was replaced by the transport classification")
	}
	if strings.Contains(wrapped.Error(), "https://pypi.org") {
		t.Fatalf("preserved refusal carried its request URL: %q", wrapped.Error())
	}
}

func TestUnparsableEndpointStillRefusesWithoutInventingAHost(t *testing.T) {
	diagnostic := singleDiagnostic(t, transportFailure("publisher metadata source", "::not a url::", errors.New("dial error")))
	if !strings.Contains(diagnostic.Message, "its approved publisher") || strings.Contains(diagnostic.Message, "not a url") {
		t.Fatalf("unparsable endpoint produced %q", diagnostic.Message)
	}
}

func TestDependencyAcquisitionReportsResolutionFailureForItsOwnSource(t *testing.T) {
	source := fixtureSource("test-artifact", []byte("approved artifact"))
	_, err := receiveSource(t.Context(), source, func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: request.URL.String(), Err: &net.DNSError{Err: "i/o timeout", Name: "files.pythonhosted.org", IsTimeout: true}}
	})
	diagnostic := singleDiagnostic(t, err)
	if !strings.Contains(diagnostic.Message, "approved dependency source files.pythonhosted.org") || !strings.Contains(diagnostic.Message, "resolver timed out") {
		t.Fatalf("dependency acquisition hid its resolution failure: %q", diagnostic.Message)
	}
}
