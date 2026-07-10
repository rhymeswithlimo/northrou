// Package broker implements Northrou's stateless remote-access signaling relay.
// It matches remote clients to home servers by connection code and relays only
// WebRTC signaling (SDP offers/answers and ICE candidates) so the two can
// hole-punch a direct peer-to-peer connection. Media data never flows through
// the broker.
package broker

// Message is the JSON envelope exchanged over the signaling WebSocket. Only
// signaling fields are ever carried, never media.
type Message struct {
	Type string `json:"type"`

	// register/connect
	Role     string `json:"role,omitempty"`      // "server" | "client"
	ServerID string `json:"server_id,omitempty"` // opaque home-server id
	Code     string `json:"code,omitempty"`      // pairing/connection code

	// session-scoped relay
	Session   string `json:"session,omitempty"`
	SDP       string `json:"sdp,omitempty"`       // offer/answer
	Candidate string `json:"candidate,omitempty"` // ICE candidate (JSON)

	Error string `json:"error,omitempty"`
}

// Message types.
const (
	TypeRegister   = "register"   // server -> broker: announce availability
	TypeRegistered = "registered" // broker -> server: ack
	TypeConnect    = "connect"    // client -> broker: request a server by code
	TypePaired     = "paired"     // broker -> both: a session was created
	TypeOffer      = "offer"      // client -> server (relayed)
	TypeAnswer     = "answer"     // server -> client (relayed)
	TypeCandidate  = "candidate"  // both directions (relayed)
	TypeError      = "error"      // broker -> peer: something went wrong
)
