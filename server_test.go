package chatroom

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func Test_NewServer(t *testing.T) {
	s := NewServer(zerolog.Nop())

	assert.NotNil(t, s.log)
	assert.NotNil(t, s.users)
	assert.NotNil(t, s.writeCh)
}

func Test_Server_handleWsConn(t *testing.T) {
	s := NewServer(zerolog.Nop())

	hs := httptest.NewServer(s)
	defer hs.Close()

	// invalid route
	resp, err := http.Get(hs.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// invalid user
	resp, err = http.Get(hs.URL + "/ws")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// duplicate user
	s.userMu.Lock()
	s.users["test"] = &websocket.Conn{}
	s.userMu.Unlock()

	resp, err = http.Get(hs.URL + "/ws?username=test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	s.userMu.Lock()
	delete(s.users, "test")
	s.userMu.Unlock()

	// cannot upgrade
	resp, err = http.Get(hs.URL + "/ws?username=test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusUpgradeRequired, resp.StatusCode)

	// successful connection and message write/read.
	ctx := context.Background()

	s.writerMu.RLock()
	assert.False(t, s.writerEnabled)
	s.writerMu.RUnlock()

	c1, _, err := websocket.Dial(ctx, hs.URL+"/ws?username=test1", nil)
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 3)

	s.userMu.RLock()
	assert.Equal(t, 1, len(s.users))
	s.userMu.RUnlock()

	s.writerMu.RLock()
	assert.True(t, s.writerEnabled)
	s.writerMu.RUnlock()

	c2, _, err := websocket.Dial(ctx, hs.URL+"/ws?username=test2", nil)
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 3)

	s.userMu.RLock()
	assert.Equal(t, 2, len(s.users))
	s.userMu.RUnlock()

	err = wsjson.Write(ctx, c1, Message{Text: "test123"})
	require.NoError(t, err)

	var msg Message
	err = wsjson.Read(ctx, c2, &msg)
	assert.NoError(t, err)

	exp := Message{
		Username: "test1",
		Text:     "test123",
	}

	assert.Equal(t, exp, msg)
}

func Test_Server_handleWrite(t *testing.T) {
	// write ctx done
	s := NewServer(zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.handleWrite(ctx)

	s.writerMu.RLock()
	assert.False(t, s.writerEnabled)
	s.writerMu.RUnlock()
}

func Test_Server_handleRead(t *testing.T) {
	// read error handling
	s := NewServer(zerolog.Nop())

	hs := httptest.NewServer(s)
	defer hs.Close()

	c, _, err := websocket.Dial(context.Background(), hs.URL+"/ws?username=test", nil)
	require.NoError(t, err)

	time.Sleep(time.Millisecond * 3)

	s.userMu.RLock()
	_, ok := s.users["test"]
	s.userMu.RUnlock()

	assert.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = s.handleRead(ctx, "test", c)
	assert.Error(t, err)

	s.userMu.RLock()
	_, ok = s.users["test"]
	s.userMu.RUnlock()

	assert.False(t, ok)
}

func Test_Server_unsubscribe(t *testing.T) {
	s := NewServer(zerolog.Nop())

	hs := httptest.NewServer(s)
	defer hs.Close()

	var cc []*websocket.Conn

	for i := 0; i < 3; i++ {
		c, _, err := websocket.Dial(context.Background(), fmt.Sprintf("%s/ws?username=%d", hs.URL, i), nil)
		require.NoError(t, err)

		cc = append(cc, c)
	}

	time.Sleep(time.Millisecond * 3)

	s.userMu.Lock()
	s.unsubscribe("4")
	s.userMu.Unlock()

	s.userMu.RLock()
	assert.Len(t, s.users, 3)
	s.userMu.RUnlock()

	ch := make(chan struct{})

	go func() {
		var msg string
		_ = wsjson.Read(context.Background(), cc[0], &msg)

		assert.NotNil(t, msg)

		close(ch)
	}()

	s.userMu.Lock()
	s.unsubscribe("0")
	s.userMu.Unlock()

	s.userMu.RLock()
	assert.Len(t, s.users, 2)
	_, ok := s.users["0"]
	assert.False(t, ok)
	s.userMu.RUnlock()

	<-ch
}
