package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

type ReloadingTLSCert struct {
	certPointer atomic.Pointer[tls.Certificate]
	certWatcher *fsnotify.Watcher
	certPath    string
	keyPath     string
	log         *slog.Logger
}

func NewReloadingTLSCert(certPath, keyPath string, log *slog.Logger) (*ReloadingTLSCert, error) {
	certWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("setting up file watcher: %w", err)
	}

	reloader := &ReloadingTLSCert{
		certPointer: atomic.Pointer[tls.Certificate]{},
		certWatcher: certWatcher,
		certPath:    certPath,
		keyPath:     keyPath,
		log:         log.With("component", "TLSReloader"),
	}

	err = reloader.loadCertificate()
	if err != nil {
		return nil, fmt.Errorf("loading certificate: %w", err)
	}

	return reloader, nil
}

func (r *ReloadingTLSCert) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return r.certPointer.Load(), nil
}

func (r *ReloadingTLSCert) loadCertificate() error {
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return fmt.Errorf("loading certificate pair: %w", err)
	}

	r.certPointer.Store(&cert)

	r.log.Info("updated current TLS certificate")

	return nil
}

func (r *ReloadingTLSCert) Start(ctx context.Context) error {
	files := []string{r.certPath, r.keyPath}

	for _, f := range files {
		if err := r.certWatcher.Add(f); err != nil {
			return fmt.Errorf("adding file to watcher: %w", err)
		}
	}

	go r.Watch()

	r.log.InfoContext(ctx, "starting certificate watcher")

	// Block until the context is done.
	<-ctx.Done()

	if err := r.certWatcher.Close(); err != nil {
		return fmt.Errorf("closing file watcher: %w", err)
	}

	return nil
}

func (r *ReloadingTLSCert) Watch() {
	for {
		select {
		case event, ok := <-r.certWatcher.Events:
			// Channel is closed.
			if !ok {
				return
			}

			r.handleEvent(event)

		case err, ok := <-r.certWatcher.Errors:
			// Channel is closed.
			if !ok {
				return
			}

			r.log.Error("certificate watch", "error", err)
		}
	}
}

func (r *ReloadingTLSCert) handleEvent(event fsnotify.Event) {
	// Only care about events which may modify the contents of the file.
	if !isWrite(event) && !isRemove(event) && !isCreate(event) {
		return
	}

	r.log.Debug("certificate event", "event", event)

	// If the file was removed, re-add the watch.
	if isRemove(event) {
		if err := r.certWatcher.Add(event.Name); err != nil {
			r.log.Error("re-watching file", "error", err)
		}
	}

	if err := r.loadCertificate(); err != nil {
		r.log.Error("re-reading certificate", "error", err)
	}
}

func isWrite(event fsnotify.Event) bool {
	return event.Op&fsnotify.Write == fsnotify.Write
}

func isCreate(event fsnotify.Event) bool {
	return event.Op&fsnotify.Create == fsnotify.Create
}

func isRemove(event fsnotify.Event) bool {
	return event.Op&fsnotify.Remove == fsnotify.Remove
}
