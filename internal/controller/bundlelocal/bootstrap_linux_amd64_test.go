//go:build linux && amd64

package bundlelocal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPublisherBrokerRefusesUnapprovedContactsBeforeDial(t *testing.T) {
	broker := &publisherBroker{ctx: context.Background()}
	for _, test := range []struct{ method, authority string }{{http.MethodGet, "pypi.org:443"}, {http.MethodConnect, "example.test:443"}, {http.MethodConnect, "pypi.org:80"}, {http.MethodConnect, "pypi.org.example.test:443"}, {http.MethodConnect, "user@files.pythonhosted.org:443"}} {
		request := httptest.NewRequest(test.method, "https://pypi.org/", nil)
		request.Host = test.authority
		response := httptest.NewRecorder()
		broker.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unapproved route contacted: %s %s", test.method, test.authority)
		}
	}
}

func TestPublisherBrokerReservesCapacityAndJoinsCanceledRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 8)
	serverDone := make(chan struct{})
	close(serverDone)
	broker := &publisherBroker{ctx: ctx, cancel: cancel, done: serverDone, server: &http.Server{}, proxy: func(*http.Request) (*url.URL, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	request := func() *http.Request {
		value := httptest.NewRequest(http.MethodConnect, "https://pypi.org/", nil)
		value.Host = "pypi.org:443"
		return value
	}
	for range 8 {
		go broker.ServeHTTP(httptest.NewRecorder(), request())
	}
	for range 8 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("reserved requests did not reach their explicit route")
		}
	}
	response := httptest.NewRecorder()
	broker.ServeHTTP(response, request())
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("an extra request passed the capacity bound before existing dials completed")
	}
	closed := make(chan struct{})
	go func() { broker.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("broker shutdown did not cancel and join active requests")
	}
	if broker.active != 0 {
		t.Fatal("broker returned before its reserved requests finished")
	}
	response = httptest.NewRecorder()
	broker.ServeHTTP(response, request())
	if response.Code != http.StatusTooManyRequests {
		t.Fatal("closed broker accepted another publisher route")
	}
}

func TestBootstrapChildHasNoHostRootMapping(t *testing.T) {
	attributes := bootstrapProcessAttributes("/disposable")
	if attributes.Chroot != "/disposable" || attributes.Cloneflags&(unix.CLONE_NEWUSER|unix.CLONE_NEWNS|unix.CLONE_NEWPID) != (unix.CLONE_NEWUSER|unix.CLONE_NEWNS|unix.CLONE_NEWPID) {
		t.Fatal("resolver confinement is incomplete")
	}
	if len(attributes.UidMappings) != 1 || attributes.UidMappings[0].HostID == 0 || attributes.UidMappings[0].ContainerID != 0 || attributes.UidMappings[0].Size != 1 || attributes.Credential == nil || attributes.Credential.Uid != 0 || attributes.GidMappingsEnableSetgroups {
		t.Fatal("resolver identity can retain host-root authority")
	}
	want := os.Getuid()
	if want == 0 {
		want = 65534
	}
	if attributes.UidMappings[0].HostID != want {
		t.Fatal("resolver mapped an unrelated host identity")
	}
}
