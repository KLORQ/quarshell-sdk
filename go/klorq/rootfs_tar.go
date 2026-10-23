package klorq

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	compatv1 "github.com/KLORQ/quarshell-proto/gen/go/openshell/gateway/v1"
)

// UploadRootfsTar creates an OpenShell staging slot and streams the rootfs tar
// to its gateway-local path. The caller must share the gateway filesystem.
// The returned token belongs in the selected driver's
// rootfs_tar_staging_token configuration for CreateSandbox.
func (c *Client) UploadRootfsTar(ctx context.Context, fileName string, size uint64, source io.Reader) (string, error) {
	if strings.TrimSpace(fileName) == "" || strings.ContainsAny(fileName, `/\\`) || fileName == "." || fileName == ".." {
		return "", fmt.Errorf("upload rootfs tar: file name must be a base name")
	}
	if size == 0 || size > math.MaxInt64 || source == nil {
		return "", fmt.Errorf("upload rootfs tar: size and source are required")
	}
	rpc, err := c.gatewayClient()
	if err != nil {
		return "", err
	}
	staging, err := rpc.BeginRootfsTarStaging(ctx, &compatv1.BeginRootfsTarStagingRequest{
		WorkspaceScope: namedTemplateWorkspace(c.workspace()), FileName: fileName, SizeBytes: size,
	})
	if err != nil {
		return "", fmt.Errorf("allocate rootfs tar staging: %w", err)
	}
	if staging.GetUploadPath() == "" {
		return "", fmt.Errorf("gateway returned an empty rootfs staging path")
	}
	if maxBytes := staging.GetMaxBytes(); maxBytes > 0 && size > maxBytes {
		return "", fmt.Errorf("rootfs tar is %d bytes, exceeding the gateway limit of %d bytes", size, maxBytes)
	}
	// OpenShell returns a gateway-local staging path. Like the upstream CLI,
	// this operation requires the client to share that filesystem.
	file, err := os.OpenFile(filepath.Clean(staging.GetUploadPath()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create rootfs tar staging file: %w", err)
	}
	written, copyErr := io.CopyN(file, &contextReader{ctx: ctx, reader: source}, int64(size))
	if copyErr == nil && written != int64(size) {
		copyErr = io.ErrUnexpectedEOF
	}
	if copyErr == nil {
		var extra [1]byte
		n, readErr := source.Read(extra[:])
		if n > 0 {
			copyErr = fmt.Errorf("rootfs tar source exceeds declared size")
		} else if readErr != nil && readErr != io.EOF {
			copyErr = readErr
		}
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(staging.GetUploadPath())
		return "", fmt.Errorf("write rootfs tar staging file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(staging.GetUploadPath())
		return "", fmt.Errorf("close rootfs tar staging file: %w", closeErr)
	}
	return staging.GetStagingToken(), nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
