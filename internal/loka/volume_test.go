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
		{"store type returns virtiofs", Volume{Type: "store"}, "virtiofs"},
		{"provider local returns virtiofs", Volume{Provider: "local"}, "virtiofs"},
		{"provider volume returns virtiofs", Volume{Provider: "volume"}, "virtiofs"},
		{"provider store returns virtiofs", Volume{Provider: "store", Name: "mystore"}, "virtiofs"},
		{"github returns virtiofs", Volume{Provider: "github", GitRepo: "owner/repo"}, "virtiofs"},
		{"git returns virtiofs", Volume{Provider: "git", GitRepo: "owner/repo"}, "virtiofs"},
		{"readonly legacy returns block", Volume{Access: "readonly", Provider: "s3", Bucket: "b"}, "block"},
		{"readwrite legacy returns fuse", Volume{Provider: "s3", Bucket: "b"}, "fuse"},
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
		{"explicit store", Volume{Type: "store"}, "store"},
		{"hostpath auto-detect", Volume{HostPath: "/data"}, "network"},
		{"provider local auto-detect", Volume{Provider: "local"}, "network"},
		{"provider volume auto-detect", Volume{Provider: "volume", Name: "mydata"}, "network"},
		{"provider store auto-detect", Volume{Provider: "store", Name: "mystore"}, "store"},
		{"s3 bucket auto-detect", Volume{Bucket: "my-bucket"}, "object"},
		{"provider s3 auto-detect", Volume{Provider: "s3"}, "object"},
		{"provider gcs auto-detect", Volume{Provider: "gcs"}, "object"},
		{"provider azure auto-detect", Volume{Provider: "azure"}, "object"},
		{"github auto-detect", Volume{Provider: "github", GitRepo: "owner/repo"}, "network"},
		{"git auto-detect", Volume{Provider: "git", GitRepo: "owner/repo"}, "network"},
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
