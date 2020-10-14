package groupchat

import (
	"context"
	"net/http"
	"sync"

	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Server holds all users information.
type Server struct {
	log zerolog.Logger

	writeCh chan Message

	userMu sync.RWMutex
	users  map[string]*websocket.Conn

	writerMu      sync.RWMutex
	writerEnabled bool
}

// Message holds all data that is sent using websockets.
type Message struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

// NewServer creates fresh instance of server.
func NewServer(log zerolog.Logger) *Server {
	s := &Server{
		log:     log,
		users:   make(map[string]*websocket.Conn),
		writeCh: make(chan Message),
	}

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.handleWsConn(w, r)
		return
	}

	http.NotFoundHandler().ServeHTTP(w, r)
}

// handleWsConn upgrades incoming websocket connections.
func (s *Server) handleWsConn(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.userMu.RLock()
	_, ok := s.users[username]
	s.userMu.RUnlock()

	if ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.log.Warn().Err(err).Msg("cannot upgrade connection")
		return
	}
	defer c.Close(websocket.StatusInternalError, "internal error")

	s.userMu.Lock()
	s.users[username] = c
	s.userMu.Unlock()

	s.log.Info().Msg("connection established")
	defer s.log.Info().Msg("connection closed")

	ctx := context.Background()

	s.writerMu.RLock()
	if !s.writerEnabled {
		go s.handleWrite(ctx)
	}
	s.writerMu.RUnlock()

	for {
		if err := s.handleRead(ctx, username, c); err != nil {
			return
		}
	}
}

//handleWrite handles sending messages to all clients.
func (s *Server) handleWrite(ctx context.Context) {
	s.writerMu.Lock()
	s.writerEnabled = true
	s.writerMu.Unlock()

	defer func() {
		s.writerMu.Lock()
		s.writerEnabled = false
		s.writerMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.writeCh:
			s.userMu.RLock()
			for username, c := range s.users {
				if username == msg.Username {
					continue
				}

				if err := wsjson.Write(ctx, c, msg); err != nil {
					s.log.Err(err).Msg("during writing process")

					s.userMu.Lock()
					s.unsubscribe(username)
					s.userMu.Unlock()
				}
			}
			s.userMu.RUnlock()
		}
	}
}

// handleRead handles reading all incoming websocket messages.
func (s *Server) handleRead(ctx context.Context, username string, c *websocket.Conn) error {
	var data Message

	if err := wsjson.Read(ctx, c, &data); err != nil {
		s.userMu.Lock()
		s.unsubscribe(username)
		s.userMu.Unlock()

		return err
	}

	data.Username = username

	s.log.Info().Msgf("received %s message: %s", data.Username, data.Text)

	s.writeCh <- data

	return nil
}

// unsubscribe removes user from subscribed users list.
func (s *Server) unsubscribe(username string) {
	c, ok := s.users[username]
	if !ok {
		return
	}

	c.Close(websocket.StatusNormalClosure, "")
	delete(s.users, username)
}
