package discord

import (
	"encoding/json"
	"time"
)

const (
	PAYLOAD_GATEWAY_DISPATCH      = 0
	PAYLOAD_GATEWAY_HEARTBEAT     = 1
	PAYLOAD_GATEWAY_IDENTIFY      = 2
	PAYLOAD_GATEWAY_STATUS_UPDATE = 3
	PAYLOAD_GATEWAY_HELLO         = 10
	PAYLOAD_GATEWAY_HEARTBEAT_ACK = 11
)

// Helper function for getting the name of a payload from its
// op code. Used for logging.
func PayloadOpToName(op int) string {
	switch op {
	case PAYLOAD_GATEWAY_DISPATCH:
		return "Dispatch"
	case PAYLOAD_GATEWAY_HEARTBEAT:
		return "Heartbeat"
	case PAYLOAD_GATEWAY_IDENTIFY:
		return "Identify"
	case PAYLOAD_GATEWAY_STATUS_UPDATE:
		return "Status Update"
	case PAYLOAD_GATEWAY_HELLO:
		return "Hello"
	case PAYLOAD_GATEWAY_HEARTBEAT_ACK:
		return "Heartbeat ACK"
	default:
		return "UNKNOWN"
	}
}

// https://discordapp.com/developers/docs/topics/gateway#gateway-op-codespayloads
type GatewayPayload struct {
	OpCode    int         `json:"op"`
	Data      interface{} `json:"d"`
	SeqNumber *int        `json:"s"`
	EventName *string     `json:"t"`
}

type RawGatewayPayload struct {
	OpCode    int             `json:"op"`
	Data      json.RawMessage `json:"d"`
	SeqNumber *int            `json:"s"`
	EventName *string         `json:"t"`
}

// Used to maintain an active gateway connection. Must be sent
// every heartbeat_interval milliseconds after the ready payload
// is received. The inner d key must be set to the last seq (s)
// received by the client. If none has yet been received you
// should send null.
// https://discordapp.com/developers/docs/topics/gateway#gateway-heartbeat
type GatewayHeartbeatData *int

// Used to trigger the initial handshake with the gateway.
// https://discordapp.com/developers/docs/topics/gateway#gateway-identify
type GatewayIdentifyData struct {
	Token      string                 `json:"token"`
	Compress   bool                   `json:"compress"`
	Properties IdentifyDataProperties `json:"properties"`
}
type IdentifyDataProperties struct {
	Os              string `json:"$os"`
	Browser         string `json:"$browser"`
	Device          string `json:"$device"`
	Referrer        string `json:"$referrer"`
	ReferringDomain string `json:"$referring_domain"`
}

// Sent by the client to indicate a presence or status update.
// https://discordapp.com/developers/docs/topics/gateway#gateway-status-update
type GatewayStatusUpdateData struct {
	IdleSince *int              `json:"idle_since"`
	Game      *StatusUpdateGame `json:"game"`
}
type StatusUpdateGame struct {
	Name string `json:"name"`
}

// Sent on connection to the websocket. Defines the
// heartbeat interval that the client should heartbeat to.
// https://discordapp.com/developers/docs/topics/gateway#gateway-hello
type GatewayHelloData struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

func NewGatewayPayload(opCode int, data interface{}, seqNumber *int, eventName *string) GatewayPayload {
	return GatewayPayload{
		OpCode:    opCode,
		Data:      data,
		SeqNumber: seqNumber,
		EventName: eventName,
	}
}
