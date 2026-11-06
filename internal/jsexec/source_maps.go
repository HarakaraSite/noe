package jsexec

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func readSourceMap(ctx context.Context, files FileSystem, script, reference string) ([]byte, string, error) {
	if ctx == nil {
		return nil, "", errors.New("source map: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	scriptURL, localScript := sourceMapScriptURL(script)
	driveReference := windowsDriveAbsolute(reference)
	var ref *url.URL
	var err error
	if !driveReference {
		ref, err = url.Parse(reference)
		if err != nil {
			return nil, "", err
		}
		if strings.EqualFold(ref.Scheme, "data") {
			data, _, err := quickjs.ReadSourceMap("", reference)
			if err != nil {
				return nil, "", err
			}
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			return data, scriptURL, nil
		}
	}

	if !localScript {
		return nil, "", errors.New("source map: sidecars require a local file script")
	}
	if files == nil {
		return nil, "", errors.New("source map: filesystem access is not selected")
	}

	var mapURL *url.URL
	if windowsDriveAbsolute(reference) {
		mapURL, err = url.Parse(fileModuleURL(reference))
		if err != nil {
			return nil, "", err
		}
	} else {
		if ref.Scheme != "" {
			return nil, "", errors.New("source map: absolute map URLs are not supported")
		}
		ref, err = url.Parse(filepath.ToSlash(reference))
		if err != nil {
			return nil, "", err
		}
		if ref.Scheme != "" {
			return nil, "", errors.New("source map: absolute map URLs are not supported")
		}
		if ref.Host != "" || strings.HasPrefix(filepath.ToSlash(reference), "//") {
			return nil, "", errors.New("source map: protocol-relative map URLs are not supported")
		}
		base, err := url.Parse(scriptURL)
		if err != nil {
			return nil, "", err
		}
		mapURL = base.ResolveReference(ref)
	}

	if !strings.EqualFold(mapURL.Scheme, "file") || mapURL.Opaque != "" {
		return nil, "", errors.New("source map: sidecars must resolve to a local file")
	}
	if !localFileHost(mapURL) {
		return nil, "", errors.New("source map: nonlocal file hosts are not supported")
	}
	mapPath, err := sourceMapFilePath(mapURL)
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	data, err := files.ReadFile(ctx, mapPath)
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return data, mapURL.String(), nil
}

// ReadSourceMap reads inline maps or sidecars through this binding's selected
// filesystem view. It is safe to call from the runtime owner.
func (b *Bindings) ReadSourceMap(script, reference string) ([]byte, string, error) {
	if b == nil {
		return nil, "", errors.New("stdlib: bindings are closed")
	}
	b.state.mu.Lock()
	closed, ctx, files := b.state.closed, b.ctx, b.fs
	b.state.mu.Unlock()
	if closed {
		return nil, "", errors.New("stdlib: bindings are closed")
	}
	if ctx == nil {
		return nil, "", errors.New("stdlib: bindings have no execution context")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return readSourceMap(ctx, files, script, reference)
}

func sourceMapScriptURL(script string) (string, bool) {
	if filepath.IsAbs(script) || strings.HasPrefix(script, "/") {
		return fileModuleURL(script), true
	}
	if windowsDriveAbsolute(script) {
		return fileModuleURL(script), true
	}
	u, err := url.Parse(script)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || !strings.HasPrefix(u.Path, "/") || !localFileHost(u) {
		// Non-file IDs remain useful as inline-map bases but never grant a
		// filesystem path for external map loading.
		return script, false
	}
	u.Scheme = "file"
	return u.String(), true
}

func localFileHost(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	return u.Host == "" || strings.EqualFold(u.Host, "localhost")
}

func sourceMapFilePath(u *url.URL) (string, error) {
	if u == nil || !strings.EqualFold(u.Scheme, "file") || !localFileHost(u) || u.Opaque != "" {
		return "", errors.New("source map: URL is not a local file")
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "", errors.New("source map: file URL path is not absolute")
	}
	if runtime.GOOS == "windows" {
		windowsPath := strings.ReplaceAll(u.Path, "\\", "/")
		if strings.HasPrefix(windowsPath, "//") {
			return "", errors.New("source map: Windows UNC paths are not supported")
		}
	}
	if runtime.GOOS == "windows" && windowsFileURLDrivePath(u.Path) {
		return filepath.FromSlash(strings.TrimPrefix(u.Path, "/")), nil
	}
	// A volume-free file URL path is the virtual path form used by Root views
	// on Windows; Backend.Resolve also maps it to the current volume otherwise.
	return u.Path, nil
}

func windowsFileURLDrivePath(path string) bool {
	return len(path) >= 4 && path[0] == '/' && isASCIIAlpha(path[1]) && path[2] == ':' && path[3] == '/'
}

func windowsDriveAbsolute(value string) bool {
	if runtime.GOOS != "windows" || len(value) < 3 || value[1] != ':' || !isASCIIAlpha(value[0]) {
		return false
	}
	return value[2] == '/' || value[2] == '\\'
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
