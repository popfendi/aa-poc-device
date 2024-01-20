package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"
)

var addr = flag.String("addr", os.Getenv("SERVER_ADDRESS"), "http service address")
var conn *websocket.Conn
var deviceID = os.Getenv("DEVICE_ID")
var peerConnection *webrtc.PeerConnection

var (
	connectionOpen bool
	connMu         sync.Mutex
)

type Message struct {
	DeviceID string `json:"deviceId"`
	ClientID string `json:"clientId"`
	Data     string `json:"data"`
	Action   string `json:"action"`
}

func main() {
	initLogger()
	dataChannel := make(chan []byte)
	go analyze(dataChannel)

	go processMessages(dataChannel)
	// Continuously try to connect and handle messages
	for {
		err := connectAndHandleMessages()
		if err != nil {
			Sugar.Errorf("Error: %v", err)
			setConnectionOpen(false)
			// Exponential backoff strategy for reconnection attempts
			time.Sleep(1 * time.Second)
			continue
		}
	}
}

func setConnectionOpen(status bool) {
	connMu.Lock()
	defer connMu.Unlock()
	connectionOpen = status
}

func isConnectionOpen() bool {
	connMu.Lock()
	defer connMu.Unlock()
	return connectionOpen
}

func setConn(newConn *websocket.Conn) {
	connMu.Lock()
	defer connMu.Unlock()
	conn = newConn
}

func getConn() *websocket.Conn {
	connMu.Lock()
	defer connMu.Unlock()
	return conn
}

func processMessages(dataChannel <-chan []byte) {
	for data := range dataChannel {
		if currentConn := getConn(); currentConn != nil && isConnectionOpen() {
			// Process the data only if the connection is open and conn is not nil
			m := Message{Action: "log", DeviceID: deviceID, Data: string(data)}
			currentConn.WriteJSON(m)
		}
	}
}

func connectAndHandleMessages() error {
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/signal"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}

	Sugar.Info("WS connected")

	setConnectionOpen(true)
	setConn(conn)

	// Register the device with the signaling server
	registerMessage := Message{
		DeviceID: deviceID,
		Action:   "register",
	}
	err = conn.WriteJSON(registerMessage)
	if err != nil {
		return err
	}

	defer func() {
		conn.Close()
		setConnectionOpen(false)
		setConn(nil)
	}()

	// Keep reading messages to keep the connection alive
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			// Handle disconnection here
			return err
		}
		// Process message
		go messageHandler(message)
	}
}

func messageHandler(message []byte) {
	var msg Message
	if err := json.Unmarshal(message, &msg); err != nil {
		Sugar.Errorf("json unmarshal err: %s", err.Error())
		return
	}

	switch msg.Action {
	case "startCall":
		fmt.Println("startcall")
		if peerConnection != nil {
			handleEndCall(peerConnection)
		}
		peerConnection = handleStartCall(conn, deviceID, msg.ClientID)
	case "endCall":
		handleEndCall(peerConnection)

	case "sdpAnswer":
		handleSDPAnswer(peerConnection, msg.Data)

	case "receiveIceCandidate":
		handleICECandidate(peerConnection, msg.Data)
	}

}

func handleStartCall(c *websocket.Conn, deviceID string, clientID string) *webrtc.PeerConnection {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	// Initialize media devices
	opusParams, _ := opus.NewParams()
	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithAudioEncoders(&opusParams),
	)

	mediaEngine := webrtc.MediaEngine{}
	codecSelector.Populate(&mediaEngine)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))

	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		Sugar.Errorf("%s", err.Error())
	}

	// Set the handler for ICE connection state
	// This will notify you when the peer has connected/disconnected
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		Sugar.Infof("Connection State has changed %s", connectionState.String())
	})
	gatherCandidates(peerConnection, conn, deviceID, clientID)

	// Get the audio source (for example, a microphone)
	audioSource, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Audio: func(c *mediadevices.MediaTrackConstraints) {
			c.ChannelCount = prop.Int(1)
		},
		Codec: codecSelector,
	})
	if err != nil {
		Sugar.Error(err.Error())
	}

	for _, track := range audioSource.GetTracks() {
		track.OnEnded(func(err error) {
			Sugar.Errorf("Track (ID: %s) ended with error: %v", track.ID(), err)
		})

		// Add this track to the peer connection
		_, err = peerConnection.AddTransceiverFromTrack(track,
			webrtc.RtpTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			},
		)
		if err != nil {
			Sugar.Error(err.Error())
		}
	}

	// Create an offer to send to the client
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		Sugar.Error(err.Error())
	}

	// Set the local description to the offer
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		Sugar.Error(err.Error())
	}

	// Prepare the offer in the specified message format
	offerJSON, err := json.Marshal(offer)
	if err != nil {
		Sugar.Errorf("Failed to marshal offer: %s", err)
	}
	msg := Message{
		DeviceID: deviceID,
		ClientID: clientID,
		Data:     string(offerJSON),
		Action:   "sdpOffer",
	}

	// Send the message to the signaling server
	err = c.WriteJSON(msg)
	if err != nil {
		Sugar.Errorf("Failed to send offer msg: %s", err)
	}

	return peerConnection
}

func gatherCandidates(peerConnection *webrtc.PeerConnection, c *websocket.Conn, deviceID string, clientID string) {
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			// All candidates gathered, possibly send a message to signal this
			return
		}
		// Serialize the candidate
		candidateJSON, _ := json.Marshal(candidate.ToJSON())
		// Wrap the candidate in the agreed message format
		msg := Message{
			DeviceID: deviceID,
			ClientID: clientID,
			Data:     string(candidateJSON),
			Action:   "sendIceCandidate",
		}

		// Send the message to the signaling server
		err := c.WriteJSON(msg)
		if err != nil {
			Sugar.Errorf("Failed to send candidate message: %s", err.Error())
		}
	})
}

func handleEndCall(peerConnection *webrtc.PeerConnection) {
	// Close the peer connection
	peerConnection.Close()
}

func handleSDPAnswer(peerConnection *webrtc.PeerConnection, sdp string) {
	// Set the remote description to the received SDP answer
	err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		Sugar.Errorf("handle SDP answer err: %s", err.Error())
	}
}

func handleICECandidate(peerConnection *webrtc.PeerConnection, candidateData string) {
	candidate := webrtc.ICECandidateInit{}
	if err := json.Unmarshal([]byte(candidateData), &candidate); err != nil {
		Sugar.Errorf("Failed to unmarshal ICE candidate: %s", err.Error())
		return
	}

	if err := peerConnection.AddICECandidate(candidate); err != nil {
		Sugar.Errorf("Failed to add ICE candidate: %s", err.Error())
	}
}
