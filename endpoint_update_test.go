package ngrok

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.ngrok.com/ngrok/v2/internal/legacy"
	"golang.ngrok.com/ngrok/v2/internal/legacy/config"
)

type fakeUpdateSession struct {
	legacy.Session
	patchErr error
}

func (s *fakeUpdateSession) AgentSessionID() string { return "ags_test" }

func (s *fakeUpdateSession) Listen(context.Context, config.Tunnel) (legacy.Tunnel, error) {
	return &fakeIdleTunnel{closed: make(chan struct{})}, nil
}

func (s *fakeUpdateSession) PatchTunnelState(string, *string, *string, *string, *bool, *string) error {
	return s.patchErr
}

// fakeIdleTunnel accepts no connections and blocks Accept until it is closed.
type fakeIdleTunnel struct {
	legacy.Tunnel
	closed    chan struct{}
	closeOnce sync.Once
}

func (t *fakeIdleTunnel) Accept() (net.Conn, error) {
	<-t.closed
	return nil, net.ErrClosed
}

func (t *fakeIdleTunnel) CloseWithContext(context.Context) error {
	t.closeOnce.Do(func() { close(t.closed) })
	return nil
}

func (t *fakeIdleTunnel) Close() error     { return t.CloseWithContext(context.Background()) }
func (t *fakeIdleTunnel) ID() string       { return "ep_test" }
func (t *fakeIdleTunnel) TunnelID() string { return "tn_test" }
func (t *fakeIdleTunnel) Name() string     { return "" }
func (t *fakeIdleTunnel) URL() string      { return "https://test.ngrok.app" }

func newUpdateableForwarder(t *testing.T, sess *fakeUpdateSession) UpdateableEndpointForwarder {
	t.Helper()

	a := &agent{sess: sess, opts: defaultAgentOpts()}
	f, err := a.Forward(t.Context(),
		WithUpstream("http://127.0.0.1:1"),
		WithDescription("before"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	uf, ok := f.(UpdateableEndpointForwarder)
	require.True(t, ok, "Forward returns an UpdateableEndpointForwarder")
	return uf
}

// waitPastCreation sleeps long enough that a later time.Now() is strictly
// after CreatedAt even on a coarse clock.
func waitPastCreation() time.Time {
	time.Sleep(5 * time.Millisecond)
	return time.Now()
}

func TestForwarderUpdateSetsUpdatedAt(t *testing.T) {
	f := newUpdateableForwarder(t, &fakeUpdateSession{})
	created := f.CreatedAt()
	before := waitPastCreation()

	desc := "after"
	require.NoError(t, f.Update(t.Context(), nil, &desc, nil, nil, nil))

	assert.Equal(t, "after", f.Description())
	assert.True(t, f.UpdatedAt().After(created), "UpdatedAt is after CreatedAt")
	assert.False(t, f.UpdatedAt().Before(before), "UpdatedAt is not before the Update call")
	assert.Equal(t, created, f.CreatedAt())
}

func TestForwarderUpdateUpstreamSetsUpdatedAt(t *testing.T) {
	f := newUpdateableForwarder(t, &fakeUpdateSession{})
	created := f.CreatedAt()
	before := waitPastCreation()

	upstream := url.URL{Scheme: "http", Host: "127.0.0.1:2"}
	f.UpdateUpstream(upstream)

	assert.Equal(t, upstream, f.UpstreamURL())
	assert.True(t, f.UpdatedAt().After(created), "UpdatedAt is after CreatedAt")
	assert.False(t, f.UpdatedAt().Before(before), "UpdatedAt is not before the UpdateUpstream call")
	assert.Equal(t, created, f.CreatedAt())
}

func TestForwarderFailedUpdateLeavesEndpointUnchanged(t *testing.T) {
	patchErr := errors.New("patch rejected")
	f := newUpdateableForwarder(t, &fakeUpdateSession{patchErr: patchErr})
	updated := f.UpdatedAt()
	waitPastCreation()

	desc := "after"
	err := f.Update(t.Context(), nil, &desc, nil, nil, nil)

	require.ErrorIs(t, err, patchErr)
	assert.Equal(t, "before", f.Description())
	assert.Equal(t, updated, f.UpdatedAt())
}

// TestForwarderConcurrentUpdatesAndGetters fails under -race if the updates
// and the getters do not share a lock.
func TestForwarderConcurrentUpdatesAndGetters(t *testing.T) {
	f := newUpdateableForwarder(t, &fakeUpdateSession{})

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			name, desc, meta, policy := "name", "desc", "meta", "policy"
			pooling := i%2 == 0
			assert.NoError(t, f.Update(t.Context(), &name, &desc, &meta, &pooling, &policy))
			f.UpdateUpstream(url.URL{Scheme: "http", Host: "127.0.0.1:3"})
		}()
		go func() {
			defer wg.Done()
			_ = f.Name()
			_ = f.Description()
			_ = f.Metadata()
			_ = f.PoolingEnabled()
			_ = f.TrafficPolicy()
			_ = f.UpdatedAt()
			_ = f.UpstreamURL()
		}()
	}
	wg.Wait()
}
