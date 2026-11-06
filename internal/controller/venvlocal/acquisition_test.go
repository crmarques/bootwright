package venvlocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestWheelRedirectCannotLeaveItsOriginalOrigin(t *testing.T) {
	for _, target := range []string{"https://other.example/wheel", "https://127.0.0.1/wheel", "https://169.254.169.254/wheel"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{target}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("x")), ContentLength: 1, Request: request}, nil
			})}
			err := receive(context.Background(), client, "https://publisher.example/wheel", runtimelock.File{Filename: "wheel", Size: 1}, io.Discard)
			if err == nil || calls != 1 {
				t.Fatalf("redirect crossed authority: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestWheelTransportConditionsSurviveMirrorWrapping(t *testing.T) {
	mirror, err := prerequisites.ParseWheelMirror("https://mirror.example/wheels")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		cause    error
		text     string
		routable bool
	}{
		{"dns", &net.DNSError{Err: "private resolver detail", Name: "private-name"}, "could not be resolved", true},
		{"certificate", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "certificate the qualified system trust store does not accept", false},
		{"timeout", &net.DNSError{Err: "timeout", IsTimeout: true}, "resolver timed out", true},
		{"unreachable", errors.New("private endpoint details"), "could not be reached", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, test.cause })}
			source := mirrored(func(ctx context.Context, file runtimelock.File, out io.Writer) error {
				return receive(ctx, client, mirror.Location(file.Filename), file, out)
			}, mirror)
			err := source(context.Background(), runtimelock.File{Filename: "wheel", Size: 1}, io.Discard)
			var failure *prerequisites.ScopedFailure
			if !errors.As(err, &failure) || failure.Routable != test.routable || !strings.Contains(failure.Message, test.text) || strings.Contains(failure.Message, "private") {
				t.Fatalf("misclassified: %#v %v", failure, err)
			}
		})
	}
}

func TestWheelSameOriginRedirectKeepsSharedClientUntouched(t *testing.T) {
	calls, redirects := 0, 0
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { redirects++; return nil }, Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://PUBLISHER.example:443/final"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: 1, Body: io.NopCloser(strings.NewReader("x")), Request: request}, nil
	})}
	if err := receive(context.Background(), client, "https://publisher.example/wheel", runtimelock.File{Filename: "wheel", Size: 1}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || redirects != 1 {
		t.Fatalf("calls=%d redirects=%d", calls, redirects)
	}
	other, _ := http.NewRequest(http.MethodGet, "https://other.example", nil)
	if err := client.CheckRedirect(other, nil); err != nil {
		t.Fatal("fetch mutated the shared redirect policy")
	}
}

func TestMirrorKeepsNonRoutableDestinationRefusal(t *testing.T) {
	mirror, _ := prerequisites.ParseWheelMirror("https://mirror.example")
	boundary := acquisition.AcquisitionBoundaryFailure("unapproved destination")
	source := mirrored(func(context.Context, runtimelock.File, io.Writer) error { return boundary }, mirror)
	if err := source(context.Background(), runtimelock.File{}, io.Discard); err != boundary {
		t.Fatal("mirror erased destination refusal")
	}
}
