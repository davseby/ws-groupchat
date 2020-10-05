package chatting

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

	writeCh chan string

	mu    sync.RWMutex
	users map[string]*websocket.Conn
}

// New creates fresh instance of server.
func New(log zerolog.Logger) *Server {
	return &Server{
		log:   log,
		users: make(map[string]*websocket.Conn),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.handleWsConn(w, r)
		return
	}

	http.NotFoundHandler().ServeHTTP(w, r)
}

// handleWs Conn upgrades incoming websocket connections.
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

	s.log.Info().Msg("connection established")
	defer s.log.Info().Msg("connection closed")

	ctx := context.Background()

	go s.handleWrite(ctx)

	for {
		s.handleRead(ctx)
	}
}

func (s *Server) handleWrite(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.writeCh:
			for user, c := range s.users {
				err := c.Write(ctx, websocket.MessageText, []byte(msg))
				if err != nil {
					s.mu.Lock()
					s.unsubscribe(user)
					s.mu.Unlock()
				}
			}
		}
	}
}

func (s *Server) handleRead(ctx context.Context) {
	var msg string

	for user, c := range s.users {
		err := wsjson.Read(ctx, c, &msg)
		if err != nil {
			s.mu.Lock()
			s.unsubscribe(user)
			s.mu.Unlock()
		}
	}
}

func (s *Server) unsubscribe(user string) {
	c, ok := s.users[user]
	if !ok {
		return
	}

	c.Close(websocket.StatusNormalClosure, "")
	delete(s.users, user)
}
