package image

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vyprai/loka/internal/loka"
	"github.com/vyprai/loka/internal/objstore"
	"github.com/vyprai/loka/internal/worker/vm"
)

const imageBucket = "images"

// Manager handles Docker image pulling, rootfs conversion, and warm snapshots.
type Manager struct {
	images    map[string]*loka.Image // In-memory for now; production uses DB.
	objStore  objstore.ObjectStore
	vmManager *vm.Manager // VM manager for creating warm snapshots (may be nil).
	dataDir   string      // Local cache directory.
	logger    *slog.Logger
}

// NewManager creates a new image manager.
func NewManager(objStore objstore.ObjectStore, dataDir string, logger *slog.Logger) *Manager {
	os.MkdirAll(filepath.Join(dataDir, "images"), 0o755)
	return &Manager{
		images:   make(map[string]*loka.Image),
		objStore: objStore,
		dataDir:  dataDir,
		logger:   logger,
	}
}

// SetVMManager sets the VM manager used for creating warm snapshots.
// Must be called before Pull if warm snapshots are desired.
func (m *Manager) SetVMManager(vmMgr *vm.Manager) {
	m.vmManager = vmMgr
}

// Pull downloads a Docker image and converts it to a Firecracker rootfs.
//
// Steps:
//   1. docker pull <reference>
//   2. docker create <reference> (create container without starting)
//   3. docker export <container> > rootfs.tar
//   4. Create ext4 image, mount, extract tar
//   5. Inject loka-supervisor binary
//   6. Upload rootfs to object store
//   7. Optionally: boot in Firecracker and create warm snapshot
func (m *Manager) Pull(ctx context.Context, reference string) (*loka.Image, error) {
	// Check if already pulled.
	for _, img := range m.images {
		if img.Reference == reference && img.Status == loka.ImageStatusReady {
			return img, nil
		}
	}

	id := uuid.New().String()[:12]
	img := &loka.Image{
		ID:        id,
		Reference: reference,
		Status:    loka.ImageStatusPulling,
		CreatedAt: time.Now(),
	}
	m.images[id] = img

	m.logger.Info("pulling image", "id", id, "reference", reference)

	// Step 1: Pull the Docker image.
	if err := runCmd(ctx, "docker", "pull", reference); err != nil {
		img.Status = loka.ImageStatusFailed
		return img, fmt.Errorf("docker pull: %w", err)
	}

	// Get digest.
	digest, _ := cmdOutput(ctx, "docker", "explore", "--format={{index .RepoDigests 0}}", reference)
	img.Digest = strings.TrimSpace(digest)

	// Step 2: Convert to rootfs.
	img.Status = loka.ImageStatusConverting

	imageDir := filepath.Join(m.dataDir, "images", id)
	os.MkdirAll(imageDir, 0o755)
	rootfsPath := filepath.Join(imageDir, "rootfs.ext4")

	if err := m.convertToRootfs(ctx, reference, rootfsPath); err != nil {
		img.Status = loka.ImageStatusFailed
		return img, fmt.Errorf("convert rootfs: %w", err)
	}

	info, _ := os.Stat(rootfsPath)
	if info != nil {
		img.SizeMB = info.Size() / (1024 * 1024)
	}
	img.RootfsPath = fmt.Sprintf("images/%s/rootfs.ext4", id) // Normalized object store key.

	// Step 3: Upload to object store.
	f, err := os.Open(rootfsPath)
	if err != nil {
		img.Status = loka.ImageStatusFailed
		return img, err
	}
	defer f.Close()
	if err := m.objStore.Put(ctx, imageBucket, img.RootfsPath, f, info.Size()); err != nil {
		img.Status = loka.ImageStatusFailed
		return img, fmt.Errorf("upload rootfs: %w", err)
	}

	// Step 4: Create warm snapshot for fast future boots.
	// Boot a temporary VM from the rootfs, wait for supervisor ready,
	// create a diff snapshot, upload compressed snapshot files to objstore.
	// Subsequent sessions/services restore from snapshot (~28ms) instead of
	// cold booting (~2s).
	if m.vmManager != nil {
		img.Status = loka.ImageStatusWarming
		memKey, stateKey, snapErr := m.createWarmSnapshot(ctx, img, rootfsPath)
		if snapErr != nil {
			m.logger.Warn("warm snapshot failed, cold boot will be used",
				"id", id, "error", snapErr)
		} else {
			img.SnapshotMem = memKey
			img.SnapshotVMState = stateKey
			m.logger.Info("warm snapshot created",
				"id", id, "mem_key", memKey, "state_key", stateKey)
		}
	}

	img.Status = loka.ImageStatusReady
	m.logger.Info("image ready",
		"id", id,
		"reference", reference,
		"size_mb", img.SizeMB,
		"warm_snapshot", img.SnapshotMem != "",
	)
	return img, nil
}

// convertToRootfs exports a Docker image to an ext4 filesystem.
func (m *Manager) convertToRootfs(ctx context.Context, reference, rootfsPath string) error {
	tmpDir, err := os.MkdirTemp("", "loka-rootfs-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, "rootfs.tar")

	// Create a temporary container and export its filesystem.
	containerID, err := cmdOutput(ctx, "docker", "create", reference, "/bin/true")
	if err != nil {
		return fmt.Errorf("docker create: %w", err)
	}
	containerID = strings.TrimSpace(containerID)
	defer runCmd(ctx, "docker", "rm", containerID)

	if err := runCmd(ctx, "docker", "export", "-o", tarPath, containerID); err != nil {
		return fmt.Errorf("docker export: %w", err)
	}

	// Create a sparse ext4 image. Sparse files only allocate disk blocks for
	// data that's actually written — a 4GB sparse file with 200MB of content
	// uses only ~200MB on disk. This gives VMs plenty of room to install
	// dependencies without wasting disk upfront.
	if err := runCmd(ctx, "truncate", "-s", "4G", rootfsPath); err != nil {
		return fmt.Errorf("create sparse image: %w", err)
	}
	if err := runCmd(ctx, "mkfs.ext4", "-F", rootfsPath); err != nil {
		return fmt.Errorf("mkfs: %w", err)
	}

	// Mount and extract. Needs root on Linux.
	mountDir := filepath.Join(tmpDir, "mount")
	os.MkdirAll(mountDir, 0o755)
	if err := runCmd(ctx, "sudo", "mount", "-o", "loop", rootfsPath, mountDir); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer runCmd(ctx, "sudo", "umount", mountDir)

	if err := runCmd(ctx, "sudo", "tar", "-xf", tarPath, "-C", mountDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Inject loka-supervisor.
	supervisorSrc := findSupervisorBinary()
	if supervisorSrc != "" {
		dst := filepath.Join(mountDir, "usr/local/bin/loka-supervisor")
		runCmd(ctx, "sudo", "cp", supervisorSrc, dst)
		runCmd(ctx, "sudo", "chmod", "+x", dst)
	}

	return nil
}

// Get returns an image by ID.
func (m *Manager) Get(id string) (*loka.Image, bool) {
	img, ok := m.images[id]
	return img, ok
}

// Register adds an image directly (used for testing and pre-cached images).
func (m *Manager) Register(img *loka.Image) {
	m.images[img.ID] = img
}

// GetByRef returns an image by Docker reference.
func (m *Manager) GetByRef(reference string) (*loka.Image, bool) {
	for _, img := range m.images {
		if img.Reference == reference && img.Status == loka.ImageStatusReady {
			return img, true
		}
	}
	return nil, false
}

// List returns all images.
func (m *Manager) List() []*loka.Image {
	imgs := make([]*loka.Image, 0, len(m.images))
	for _, img := range m.images {
		imgs = append(imgs, img)
	}
	return imgs
}

// Delete removes an image and its warm snapshot files.
func (m *Manager) Delete(id string) error {
	img, ok := m.images[id]
	if !ok {
		return fmt.Errorf("image not found")
	}
	// Remove rootfs from object store.
	m.objStore.Delete(context.Background(), imageBucket, img.RootfsPath)
	// Remove warm snapshot files from object store.
	if img.SnapshotMem != "" {
		m.objStore.Delete(context.Background(), imageBucket, img.SnapshotMem)
	}
	if img.SnapshotVMState != "" {
		m.objStore.Delete(context.Background(), imageBucket, img.SnapshotVMState)
	}
	// Remove local cache (includes rootfs and snapshot cache).
	os.RemoveAll(filepath.Join(m.dataDir, "images", id))
	os.RemoveAll(filepath.Join(m.dataDir, "cache", "images", id))
	delete(m.images, id)
	return nil
}

// ResolveRootfsPath returns the local cache path for an image's rootfs,
// downloading from object store on cache miss.
func (m *Manager) ResolveRootfsPath(ctx context.Context, imageID string) (string, error) {
	img, ok := m.images[imageID]
	if !ok {
		return "", fmt.Errorf("image %s not found", imageID)
	}

	localPath := filepath.Join(m.dataDir, "cache", "images", imageID, "rootfs.ext4")
	if _, err := os.Stat(localPath); err == nil {
		return localPath, nil // Already cached locally.
	}

	// Download from object store.
	reader, err := m.objStore.Get(ctx, imageBucket, img.RootfsPath)
	if err != nil {
		return "", fmt.Errorf("download rootfs: %w", err)
	}
	defer reader.Close()

	os.MkdirAll(filepath.Dir(localPath), 0o755)
	f, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := f.ReadFrom(reader); err != nil {
		return "", err
	}

	return localPath, nil
}

// ResolveSnapshotPaths returns local cache paths for an image's warm snapshot
// files, downloading and decompressing from object store on cache miss.
// Returns ("", "", nil) if the image has no warm snapshot.
func (m *Manager) ResolveSnapshotPaths(ctx context.Context, imageID string) (memPath, statePath string, err error) {
	img, ok := m.images[imageID]
	if !ok {
		return "", "", fmt.Errorf("image %s not found", imageID)
	}
	if img.SnapshotMem == "" || img.SnapshotVMState == "" {
		return "", "", nil // No warm snapshot available.
	}

	cacheDir := filepath.Join(m.dataDir, "cache", "images", imageID)
	os.MkdirAll(cacheDir, 0o755)

	memPath = filepath.Join(cacheDir, "snapshot_mem")
	statePath = filepath.Join(cacheDir, "snapshot_vmstate")

	// Download and decompress memory file if not cached.
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if dlErr := m.downloadGunzip(ctx, imageBucket, img.SnapshotMem, memPath); dlErr != nil {
			return "", "", fmt.Errorf("download snapshot mem: %w", dlErr)
		}
	}

	// Download and decompress vmstate file if not cached.
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		if dlErr := m.downloadGunzip(ctx, imageBucket, img.SnapshotVMState, statePath); dlErr != nil {
			return "", "", fmt.Errorf("download snapshot vmstate: %w", dlErr)
		}
	}

	return memPath, statePath, nil
}

// createWarmSnapshot boots a temporary VM from the image rootfs, waits for
// the supervisor to become ready, creates a diff snapshot, compresses and
// uploads the snapshot files to objstore, then kills the temporary VM.
func (m *Manager) createWarmSnapshot(ctx context.Context, img *loka.Image, rootfsPath string) (memKey, stateKey string, err error) {
	tmpID := fmt.Sprintf("warmsnap-%s", img.ID)

	m.logger.Info("creating warm snapshot", "image", img.ID, "tmp_vm", tmpID)

	// Boot the temporary VM.
	microVM, err := m.vmManager.Launch(ctx, tmpID, vm.VMConfig{
		VCPU:       1,
		MemoryMB:   512,
		RootfsPath: rootfsPath,
	})
	if err != nil {
		return "", "", fmt.Errorf("launch temp VM: %w", err)
	}
	// Always clean up the temporary VM.
	defer m.vmManager.Stop(tmpID)

	// Wait for supervisor to become ready via vsock ping.
	vsock := vm.NewVsockClient(microVM.VsockPath)
	backoff := 100 * time.Millisecond
	supervisorReady := false
	for i := 0; i < 50; i++ {
		if err := vsock.Ping(); err == nil {
			supervisorReady = true
			break
		}
		time.Sleep(backoff)
		if backoff < 2*time.Second {
			backoff = time.Duration(float64(backoff) * 1.5)
		}
	}
	if !supervisorReady {
		return "", "", fmt.Errorf("supervisor did not respond to ping within timeout")
	}

	m.logger.Info("supervisor ready, creating diff snapshot", "image", img.ID)

	// Create diff snapshot (pauses the VM).
	memPath, statePath, err := m.vmManager.CreateDiffSnapshot(tmpID)
	if err != nil {
		return "", "", fmt.Errorf("create diff snapshot: %w", err)
	}

	// Compress and upload memory file.
	memKey = fmt.Sprintf("images/%s/snapshot_mem.gz", img.ID)
	if err := m.gzipUpload(ctx, memPath, imageBucket, memKey); err != nil {
		return "", "", fmt.Errorf("upload snapshot mem: %w", err)
	}

	// Compress and upload vmstate file.
	stateKey = fmt.Sprintf("images/%s/snapshot_vmstate.gz", img.ID)
	if err := m.gzipUpload(ctx, statePath, imageBucket, stateKey); err != nil {
		return "", "", fmt.Errorf("upload snapshot vmstate: %w", err)
	}

	return memKey, stateKey, nil
}

// gzipUpload compresses a local file with gzip and uploads it to objstore.
func (m *Manager) gzipUpload(ctx context.Context, localPath, bucket, key string) error {
	src, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer src.Close()

	// Compress to a temp file first so we know the size.
	tmpFile, err := os.CreateTemp("", "loka-snap-*.gz")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	gw := gzip.NewWriter(tmpFile)
	if _, err := io.Copy(gw, src); err != nil {
		tmpFile.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Upload the compressed file.
	info, err := os.Stat(tmpFile.Name())
	if err != nil {
		return err
	}
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		return err
	}
	defer f.Close()

	return m.objStore.Put(ctx, bucket, key, f, info.Size())
}

// downloadGunzip downloads a gzipped file from objstore and decompresses it
// to the given local path.
func (m *Manager) downloadGunzip(ctx context.Context, bucket, key, localPath string) error {
	reader, err := m.objStore.Get(ctx, bucket, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	gr, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	os.MkdirAll(filepath.Dir(localPath), 0o755)
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, gr); err != nil {
		return err
	}
	return nil
}

// ── Helpers ─────────────────────────────────────────────

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func findSupervisorBinary() string {
	candidates := []string{
		"bin/linux-amd64/loka-supervisor",
		"/usr/local/bin/loka-supervisor",
		"bin/loka-supervisor",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
