package node

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/anycable/anycable-go/common"
	"github.com/anycable/anycable-go/utils"
	"github.com/apex/log"
	"github.com/gorilla/websocket"
)

const (
	// CloseNormalClosure indicates normal closure
	CloseNormalClosure = websocket.CloseNormalClosure

	// CloseInternalServerErr indicates closure because of internal error
	CloseInternalServerErr = websocket.CloseInternalServerErr

	// CloseAbnormalClosure indicates abnormal close
	CloseAbnormalClosure = websocket.CloseAbnormalClosure

	// CloseGoingAway indicates closing because of server shuts down or client disconnects
	CloseGoingAway = websocket.CloseGoingAway

	writeWait    = 10 * time.Second
	pingInterval = 3 * time.Second
)

var (
	expectedCloseStatuses = []int{
		websocket.CloseNormalClosure,    // Reserved in case ActionCable fixes its behaviour
		websocket.CloseGoingAway,        // Web browser page was closed
		websocket.CloseNoStatusReceived, // ActionCable don't care about closing
	}
)

type frameType int

const (
	textFrame  frameType = 0
	closeFrame frameType = 1
)

type sentFrame struct {
	frameType   frameType
	payload     []byte
	closeCode   int
	closeReason string
}

// Session represents active client
type Session struct {
	node          *Node
	ws            *websocket.Conn
	env           *common.SessionEnv
	subscriptions map[string]bool
	send          chan sentFrame
	closed        bool
	connected     bool
	mu            sync.Mutex
	pingTimer     *time.Timer

	UID         string
	Identifiers string
	Log         *log.Entry
}

type pingMessage struct {
	Type    string      `json:"type"`
	Message interface{} `json:"message"`
}

func (p *pingMessage) toJSON() []byte {
	jsonStr, err := json.Marshal(&p)
	if err != nil {
		panic("Failed to build ping JSON 😲")
	}
	return jsonStr
}

// NewSession build a new Session struct from ws connetion and http request
func NewSession(node *Node, ws *websocket.Conn, url string, headers map[string]string, uid string) (*Session, error) {
	session := &Session{
		node:          node,
		ws:            ws,
		env:           common.NewSessionEnv(url, &headers),
		subscriptions: make(map[string]bool),
		send:          make(chan sentFrame, 256),
		closed:        false,
		connected:     false,
	}

	session.UID = uid

	ctx := node.log.WithFields(log.Fields{
		"sid": session.UID,
	})

	session.Log = ctx

	err := node.Authenticate(session)

	if err != nil {
		defer session.Close("Auth Error", CloseInternalServerErr)
	}

	go session.SendMessages()

	session.addPing()

	return session, err
}

// SendMessages waits for incoming messages and send them to the client connection
func (s *Session) SendMessages() {
	defer s.Disconnect("Write Failed", CloseAbnormalClosure)
	for {
		select {
		case message, ok := <-s.send:
			if !ok {
				return
			}

			switch message.frameType {
			case textFrame:
				err := s.write(message.payload, time.Now().Add(writeWait))

				if err != nil {
					return
				}
			case closeFrame:
				utils.CloseWS(s.ws, message.closeCode, message.closeReason)
				return
			default:
				s.Log.Errorf("Unknown frame type: %v", message)
				return
			}
		}
	}
}

// Send data to client connection
func (s *Session) Send(msg []byte) {
	s.sendFrame(&sentFrame{frameType: textFrame, payload: msg})
}

func (s *Session) sendClose(reason string, code int) {
	s.sendFrame(&sentFrame{
		frameType:   closeFrame,
		closeReason: reason,
		closeCode:   code,
	})
}

func (s *Session) sendFrame(frame *sentFrame) {
	s.mu.Lock()

	if s.send == nil {
		s.mu.Unlock()
		return
	}

	select {
	case s.send <- *frame:
	default:
		if s.send != nil {
			close(s.send)
			defer s.Disconnect("Write failed", CloseAbnormalClosure)
		}

		s.send = nil
	}

	s.mu.Unlock()
}

func (s *Session) write(message []byte, deadline time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ws.SetWriteDeadline(deadline)

	w, err := s.ws.NextWriter(websocket.TextMessage)

	if err != nil {
		return err
	}

	w.Write(message)

	return w.Close()
}

// ReadMessages reads messages from ws connection and send them to node
func (s *Session) ReadMessages() {
	for {
		_, message, err := s.ws.ReadMessage()

		if err != nil {
			if websocket.IsCloseError(err, expectedCloseStatuses...) {
				s.Log.Debugf("Websocket closed: %v", err)
				s.Disconnect("Read closed", CloseNormalClosure)
			} else {
				s.Log.Debugf("Websocket close error: %v", err)
				s.Disconnect("Read failed", CloseAbnormalClosure)
			}
			break
		}

		s.node.HandleCommand(s, message)
	}
}

// Disconnect enqueues RPC disconnect request and closes the connection
func (s *Session) Disconnect(reason string, code int) {
	s.mu.Lock()
	if s.connected {
		defer s.node.Disconnect(s)
	}
	s.connected = false
	s.mu.Unlock()

	s.Close(reason, code)
}

// Close websocket connection with the specified reason
func (s *Session) Close(reason string, code int) {
	s.mu.Lock()

	if s.closed {
		s.mu.Unlock()
		return
	}

	s.closed = true
	s.mu.Unlock()

	s.sendClose(reason, code)

	if s.pingTimer != nil {
		s.pingTimer.Stop()
	}
}

func (s *Session) sendPing() {
	deadline := time.Now().Add(pingInterval / 2)
	err := s.write(newPingMessage(), deadline)

	if err == nil {
		s.addPing()
	} else {
		s.Disconnect("Ping failed", CloseAbnormalClosure)
	}
}

func (s *Session) addPing() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	s.pingTimer = time.AfterFunc(pingInterval, s.sendPing)
}

func newPingMessage() []byte {
	return (&pingMessage{Type: "ping", Message: time.Now().Unix()}).toJSON()
}
