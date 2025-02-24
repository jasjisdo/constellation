/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package k8sapi

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/edgelesssys/constellation/internal/retry"
	"github.com/spf13/afero"
	"golang.org/x/text/transform"
	"k8s.io/utils/clock"
)

const (
	// determines the period after which retryDownloadToTempDir will retry a download.
	downloadInterval = 10 * time.Millisecond
)

// osInstaller installs binary components of supported kubernetes versions.
type osInstaller struct {
	fs      *afero.Afero
	hClient httpClient
	// clock is needed for testing purposes
	clock clock.WithTicker
	// retriable is the function used to check if an error is retriable. Needed for testing.
	retriable func(error) bool
}

// newOSInstaller creates a new osInstaller.
func newOSInstaller() *osInstaller {
	return &osInstaller{
		fs:        &afero.Afero{Fs: afero.NewOsFs()},
		hClient:   &http.Client{},
		clock:     clock.RealClock{},
		retriable: isRetriable,
	}
}

// Install downloads a resource from a URL, applies any given text transformations and extracts the resulting file if required.
// The resulting file(s) are copied to all destinations.
func (i *osInstaller) Install(
	ctx context.Context, sourceURL string, destinations []string, perm fs.FileMode,
	extract bool, transforms ...transform.Transformer,
) error {
	tempPath, err := i.retryDownloadToTempDir(ctx, sourceURL, transforms...)
	if err != nil {
		return err
	}
	defer func() {
		_ = i.fs.Remove(tempPath)
	}()
	for _, destination := range destinations {
		var err error
		if extract {
			err = i.extractArchive(tempPath, destination, perm)
		} else {
			err = i.copy(tempPath, destination, perm)
		}
		if err != nil {
			return fmt.Errorf("installing from %q: copying to destination %q: %w", sourceURL, destination, err)
		}
	}
	return nil
}

// extractArchive extracts tar gz archives to a prefixed destination.
func (i *osInstaller) extractArchive(archivePath, prefix string, perm fs.FileMode) error {
	archiveFile, err := i.fs.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive file: %w", err)
	}
	defer archiveFile.Close()
	gzReader, err := gzip.NewReader(archiveFile)
	if err != nil {
		return fmt.Errorf("reading archive file as gzip: %w", err)
	}
	defer gzReader.Close()
	if err := i.fs.MkdirAll(prefix, fs.ModePerm); err != nil {
		return fmt.Errorf("creating prefix folder: %w", err)
	}
	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parsing tar header: %w", err)
		}
		if err := verifyTarPath(header.Name); err != nil {
			return fmt.Errorf("verifying tar path %q: %w", header.Name, err)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if len(header.Name) == 0 {
				return errors.New("cannot create dir for empty path")
			}
			prefixedPath := path.Join(prefix, header.Name)
			if err := i.fs.Mkdir(prefixedPath, fs.FileMode(header.Mode)&perm); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("creating folder %q: %w", prefixedPath, err)
			}
		case tar.TypeReg:
			if len(header.Name) == 0 {
				return errors.New("cannot create file for empty path")
			}
			prefixedPath := path.Join(prefix, header.Name)
			out, err := i.fs.OpenFile(prefixedPath, os.O_WRONLY|os.O_CREATE, fs.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file %q for writing: %w", prefixedPath, err)
			}
			defer out.Close()
			if _, err := io.Copy(out, tarReader); err != nil {
				return fmt.Errorf("writing extracted file contents: %w", err)
			}
		case tar.TypeSymlink:
			if err := verifyTarPath(header.Linkname); err != nil {
				return fmt.Errorf("invalid tar path %q: %w", header.Linkname, err)
			}
			if len(header.Name) == 0 {
				return errors.New("cannot symlink file for empty oldname")
			}
			if len(header.Linkname) == 0 {
				return errors.New("cannot symlink file for empty newname")
			}
			if symlinker, ok := i.fs.Fs.(afero.Symlinker); ok {
				if err := symlinker.SymlinkIfPossible(path.Join(prefix, header.Name), path.Join(prefix, header.Linkname)); err != nil {
					return fmt.Errorf("creating symlink: %w", err)
				}
			} else {
				return errors.New("fs does not support symlinks")
			}
		default:
			return fmt.Errorf("unsupported tar record: %v", header.Typeflag)
		}
	}
}

func (i *osInstaller) retryDownloadToTempDir(ctx context.Context, url string, transforms ...transform.Transformer) (fileName string, someError error) {
	doer := downloadDoer{
		url:        url,
		transforms: transforms,
		downloader: i,
	}

	// Retries are canceled as soon as the context is canceled.
	// We need to call NewIntervalRetrier with a clock argument so that the tests can fake the clock by changing the osInstaller clock.
	retrier := retry.NewIntervalRetrier(&doer, downloadInterval, i.retriable, i.clock)
	if err := retrier.Do(ctx); err != nil {
		return "", fmt.Errorf("retrying downloadToTempDir: %w", err)
	}

	return doer.path, nil
}

// downloadToTempDir downloads a file to a temporary location, applying transform on-the-fly.
func (i *osInstaller) downloadToTempDir(ctx context.Context, url string, transforms ...transform.Transformer) (fileName string, retErr error) {
	out, err := afero.TempFile(i.fs, "", "")
	if err != nil {
		return "", fmt.Errorf("creating destination temp file: %w", err)
	}
	// Remove the created file if an error occurs.
	defer func() {
		if retErr != nil {
			_ = i.fs.Remove(fileName)
			retErr = &retriableError{err: retErr} // mark any error after this point as retriable
		}
	}()
	defer out.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("request to download %q: %w", url, err)
	}
	resp, err := i.hClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request to download %q: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request to download %q failed with status code: %v", url, resp.Status)
	}
	defer resp.Body.Close()

	transformReader := transform.NewReader(resp.Body, transform.Chain(transforms...))

	if _, err = io.Copy(out, transformReader); err != nil {
		return "", fmt.Errorf("downloading %q: %w", url, err)
	}
	return out.Name(), nil
}

// copy copies a file from oldname to newname.
func (i *osInstaller) copy(oldname, newname string, perm fs.FileMode) (err error) {
	old, openOldErr := i.fs.OpenFile(oldname, os.O_RDONLY, fs.ModePerm)
	if openOldErr != nil {
		return fmt.Errorf("copying %q to %q: cannot open source file for reading: %w", oldname, newname, openOldErr)
	}
	defer func() { _ = old.Close() }()
	// create destination path if not exists
	if err := i.fs.MkdirAll(path.Dir(newname), fs.ModePerm); err != nil {
		return fmt.Errorf("copying %q to %q: unable to create destination folder: %w", oldname, newname, err)
	}
	new, openNewErr := i.fs.OpenFile(newname, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, perm)
	if openNewErr != nil {
		return fmt.Errorf("copying %q to %q: cannot open destination file for writing: %w", oldname, newname, openNewErr)
	}
	defer func() {
		_ = new.Close()
		if err != nil {
			_ = i.fs.Remove(newname)
		}
	}()
	if _, err := io.Copy(new, old); err != nil {
		return fmt.Errorf("copying %q to %q: copying file contents: %w", oldname, newname, err)
	}

	return nil
}

type downloadDoer struct {
	url        string
	transforms []transform.Transformer
	downloader downloader
	path       string
}

type downloader interface {
	downloadToTempDir(ctx context.Context, url string, transforms ...transform.Transformer) (string, error)
}

func (d *downloadDoer) Do(ctx context.Context) error {
	path, err := d.downloader.downloadToTempDir(ctx, d.url, d.transforms...)
	d.path = path
	return err
}

// retriableError is an error that can be retried.
type retriableError struct{ err error }

func (e *retriableError) Error() string {
	return fmt.Sprintf("retriable error: %s", e.err.Error())
}

func (e *retriableError) Unwrap() error { return e.err }

// isRetriable returns true if the action resulting in this error can be retried.
func isRetriable(err error) bool {
	retriableError := &retriableError{}
	return errors.As(err, &retriableError)
}

// verifyTarPath checks if a tar path is valid (must not contain ".." as path element).
func verifyTarPath(pat string) error {
	n := len(pat)
	r := 0
	for r < n {
		switch {
		case os.IsPathSeparator(pat[r]):
			// empty path element
			r++
		case pat[r] == '.' && (r+1 == n || os.IsPathSeparator(pat[r+1])):
			// . element
			r++
		case pat[r] == '.' && pat[r+1] == '.' && (r+2 == n || os.IsPathSeparator(pat[r+2])):
			// .. element
			return errors.New("path contains \"..\"")
		default:
			// skip to next path element
			for r < n && !os.IsPathSeparator(pat[r]) {
				r++
			}
		}
	}
	return nil
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}
