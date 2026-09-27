package mqtt

// SchemeForPort is the transport for a broker port: TLS on 8883, the port
// registered for MQTT over TLS, plain TCP otherwise.
//
// The cloud sync connected in plain TCP whatever the port. A cloud broker is
// almost always on 8883 and speaks only TLS there, so the connection failed
// at the handshake every time, while the settings page labeled the field
// "TLS / TCP port".
func SchemeForPort(port int) string {
	if port == 8883 {
		return "ssl"
	}
	return "tcp"
}
