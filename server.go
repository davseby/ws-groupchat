package chatroom

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

	mu    sync.RWMutex
	users map[string]*websocket.Conn

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
	user := r.URL.Query().Get("username")
	if user == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	_, ok := s.users[user]
	s.mu.RUnlock()

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

	s.mu.Lock()
	s.users[user] = c
	s.mu.Unlock()

	s.log.Info().Msg("connection established")
	defer s.log.Info().Msg("connection closed")

	ctx := context.Background()

	if !s.writerEnabled {
		go s.handleWrite(ctx)
	}

	for {
		if err := s.handleRead(ctx, user, c); err != nil {
			return
		}
	}
}

//handleWrite handles sending messages to all clients.
func (s *Server) handleWrite(ctx context.Context) {
	s.writerEnabled = true

	defer func() {
		s.writerEnabled = false
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.writeCh:
			for user, c := range s.users {
				if user == msg.Username {
					continue
				}

				if err := wsjson.Write(ctx, c, msg); err != nil {
					s.log.Err(err).Msg("during writing process")

					s.mu.Lock()
					s.unsubscribe(user)
					s.mu.Unlock()
				}
			}
		}
	}
}

// handleRead handles reading all incoming websocket messages.
func (s *Server) handleRead(ctx context.Context, user string, c *websocket.Conn) error {
	var data Message

	if err := wsjson.Read(ctx, c, &data); err != nil {
		s.mu.Lock()
		s.unsubscribe(user)
		s.mu.Unlock()

		return err
	}

	data.Username = user

	s.log.Info().Msgf("received %s message: %s", data.Username, data.Text)

	s.writeCh <- data

	return nil
}

// unsubscribe removes user from subscribed users list.
func (s *Server) unsubscribe(user string) {
	c, ok := s.users[user]
	if !ok {
		return
	}

	c.Close(websocket.StatusNormalClosure, "")
	delete(s.users, user)
}
