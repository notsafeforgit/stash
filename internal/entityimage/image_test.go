package entityimage

import (
	"context"
	"errors"
	"testing"
)

func TestFormatDetection(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		heic bool
		webp bool
	}{
		{
			name: "heic major brand",
			data: append([]byte{0, 0, 0, 24}, []byte("ftypheic0000")...),
			heic: true,
		},
		{
			name: "heic compatible brand",
			data: append([]byte{0, 0, 0, 24}, []byte("ftypmif1heic")...),
			heic: true,
		},
		{
			name: "avif is not treated as heic",
			data: append([]byte{0, 0, 0, 24}, []byte("ftypavifmif1")...),
		},
		{
			name: "webp",
			data: []byte("RIFF0000WEBP"),
			webp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsHEIC(tt.data); got != tt.heic {
				t.Fatalf("IsHEIC() = %v, want %v", got, tt.heic)
			}
			if got := IsWebP(tt.data); got != tt.webp {
				t.Fatalf("IsWebP() = %v, want %v", got, tt.webp)
			}
		})
	}
}

func TestNormalizeRejectsHEICWhenNotAllowed(t *testing.T) {
	data := append([]byte{0, 0, 0, 24}, []byte("ftypheic0000")...)

	_, err := Normalize(context.Background(), nil, data, NormalizeOptions{})
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Normalize() error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNormalizeKeepsWebP(t *testing.T) {
	data := []byte("RIFF0000WEBP")

	got, err := Normalize(context.Background(), nil, data, NormalizeOptions{})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Normalize() = %q, want %q", got, data)
	}
}
