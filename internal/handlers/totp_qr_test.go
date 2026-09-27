package handlers

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	totpLib "github.com/pquerna/otp/totp"
)

// The enrolment QR is drawn by the server. It used to be fetched from
// api.qrserver.com with the secret in the URL.
func TestTheTOTPQRCodeIsDrawnLocally(t *testing.T) {
	key, err := totpLib.Generate(totpLib.GenerateOpts{Issuer: "OpenEdge", AccountName: "op"})
	if err != nil {
		t.Fatal(err)
	}
	u := totpQRDataURL(key)
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(u, prefix) {
		t.Fatalf("not a PNG data URL: %.40s", u)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(u, prefix))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 240 || b.Dy() != 240 {
		t.Errorf("QR is %dx%d", b.Dx(), b.Dy())
	}
}
