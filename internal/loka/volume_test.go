package loka

import "testing"

func TestVolumeEffectiveMode(t *testing.T) {
	tests := []struct {
		name string
		vol  Volume
		want string
	}{
		{"hostpath returns virtiofs", Volume{HostPath: "/data"}, "virtiofs"},
		{"network type returns virtiofs", Volume{Type: "network"}, "virtiofs"},
		{"object type returns virtiofs", Volume{Type: "object"}, "virtiofs"},
		{"readonly legacy returns block", Volume{Access: "readonly", Provider: "s3", Bucket: "b"}, "block"},
		{"readwrite legacy returns fuse", Volume{Provider: "s3", Bucket: "b"}, "fuse"},
		{"readwrite default returns fuse", Volume{Provider: "volume"}, "fuse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.vol.EffectiveMode()
			if got != tt.want {
				t.Errorf("EffectiveMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVolumeEffectiveType(t *testing.T) {
	tests := []struct {
		name string
		vol  Volume
		want string
	}{
		{"explicit network", Volume{Type: "network"}, "network"},
		{"explicit object", Volume{Type: "object"}, "object"},
		{"nfs server auto-detect", Volume{NFSServer: "10.0.0.1"}, "network"},
		{"provider nfs auto-detect", Volume{Provider: "nfs"}, "network"},
		{"hostpath auto-detect", Volume{HostPath: "/data"}, "network"},
		{"provider local auto-detect", Volume{Provider: "local"}, "network"},
		{"s3 bucket auto-detect", Volume{Bucket: "my-bucket"}, "object"},
		{"provider s3 auto-detect", Volume{Provider: "s3"}, "object"},
		{"provider gcs auto-detect", Volume{Provider: "gcs"}, "object"},
		{"provider azure auto-detect", Volume{Provider: "azure"}, "object"},
		{"named volume defaults to network", Volume{Provider: "volume", Name: "mydata"}, "network"},
		{"empty defaults to network", Volume{}, "network"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.vol.EffectiveType()
			if got != tt.want {
				t.Errorf("EffectiveType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVolumeIsReadOnly(t *testing.T) {
	if !(Volume{Access: "readonly"}).IsReadOnly() {
		t.Error("expected readonly")
	}
	if (Volume{Access: "readwrite"}).IsReadOnly() {
		t.Error("expected not readonly")
	}
	if (Volume{}).IsReadOnly() {
		t.Error("default should not be readonly")
	}
}
