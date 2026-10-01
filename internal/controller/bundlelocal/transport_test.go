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

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func singleDiagnostic(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	diagnostics := diagnostics.Of(err)
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
		routable  bool
	}{
		{"resolver timeout", &net.DNSError{Err: "i/o timeout", Name: "raw.githubusercontent.com", IsTimeout: true}, "resolver timed out", "Make raw.githubusercontent.com resolvable", true},
		{"unknown name", &net.DNSError{Err: "no such host", Name: "raw.githubusercontent.com", IsNotFound: true}, "could not be resolved", "Make raw.githubusercontent.com resolvable", true},
		{"untrusted certificate", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "qualified system trust store does not accept", "certificate authority in the system trust store", false},
		{"unknown authority", x509.UnknownAuthorityError{}, "qualified system trust store does not accept", "certificate authority in the system trust store", false},
		{"response timeout", context.DeadlineExceeded, "bounded acquisition timeout", "Restore this host's access to raw.githubusercontent.com", true},
		{"refused", errors.New("connect: connection refused"), "could not be reached from this host", "Restore this host's access to raw.githubusercontent.com", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := transportFailure("bootstrap publisher", endpoint, &url.Error{Op: "Get", URL: endpoint, Err: test.cause})
			diagnostic := singleDiagnostic(t, failure)
			if diagnostic.Code != "controller.setup" || !strings.Contains(diagnostic.Message, "bootstrap publisher raw.githubusercontent.com") || !strings.Contains(diagnostic.Message, test.condition) {
				t.Fatalf("message did not classify the failing publisher: %q", diagnostic.Message)
			}
			if !strings.Contains(diagnostic.Remediation, test.next) {
				t.Fatalf("remediation is not the action this condition needs: %q", diagnostic.Remediation)
			}
			if strings.Contains(diagnostic.Message+diagnostic.Remediation, "/astral-sh/") || strings.Contains(diagnostic.Message+diagnostic.Remediation, test.cause.Error()) {
				t.Fatalf("diagnostic leaked the request path or transport text: %q %q", diagnostic.Message, diagnostic.Remediation)
			}
			// Setup reads its route from the environment that invoked it, so
			// it names those variables and never a Machine it does not read.
			setup := diagnostic.Remediation
			if !strings.HasSuffix(setup, ", then rerun bootwright setup.") || strings.Contains(setup, "Machine") ||
				test.routable != strings.Contains(setup, ", or set HTTPS_PROXY to a proxy that reaches it and keep it out of NO_PROXY,") {
				t.Fatalf("setup's remedy names a route or command setup does not take: %q", setup)
			}
			// A context's controller stage is what settles its own
			// acquisition, over the Machine proxy choice its block froze,
			// which nothing can change while the failed apply holds it, so
			// it is offered the correction and no route.
			stage := singleDiagnostic(t, prerequisites.InStage(failure, "lab"))
			if stage.Code != diagnostic.Code || stage.Message != diagnostic.Message || !strings.Contains(stage.Remediation, test.next) ||
				!strings.HasSuffix(stage.Remediation, ", then run bootwright apply --stage controller --context lab.") ||
				strings.Contains(stage.Remediation, "setup") || strings.Contains(stage.Remediation, "PROXY") ||
				strings.Contains(stage.Remediation, "Proxy") || strings.Contains(stage.Remediation, ", or ") {
				t.Fatalf("the stage's remedy names a route or command the stage does not take: %q", stage.Remediation)
			}
		})
	}
}

// A tool catalog failure is reached only through a context's controller
// stage, which setup cannot settle because it installs no target tool.
func TestToolCatalogFailuresNameTheStageThatReachedThem(t *testing.T) {
	request := controller.ToolRequest{Kind: "helm", Version: "latest"}
	endpoint, _, err := sourceURL(request, "v4.2.10")
	if err != nil {
		t.Fatal(err)
	}
	source := prerequisites.DependencySource{ID: toolSourcePrefix(request) + "v4.2.10", URL: endpoint, SHA256: strings.Repeat("a", 64), Bytes: 1024}
	conflict := source
	conflict.SHA256 = strings.Repeat("b", 64)
	_, _, err = (&ToolCatalog{}).Select([]controller.ToolRequest{request}, []prerequisites.DependencySource{source, conflict})
	stage := singleDiagnostic(t, prerequisites.InStage(err, "lab"))
	if stage.Code != "controller.setup" || stage.Message != "retained tool release has conflicting publisher identities" ||
		stage.Remediation != "Restore approved dependency sources or the exact retained bundle, then run bootwright apply --stage controller --context lab." {
		t.Fatalf("stage failure: %+v", stage)
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
	if stage := singleDiagnostic(t, prerequisites.InStage(wrapped, "lab")); !strings.HasSuffix(stage.Remediation, ", then run bootwright apply --stage controller --context lab.") {
		t.Fatalf("a preserved refusal lost the scope that renders its remedy: %q", stage.Remediation)
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
