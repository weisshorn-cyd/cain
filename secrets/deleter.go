package secrets

import (
	"context"
	"log/slog"

	kErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/weisshorn-cyd/cain/utils"
)

// Deleter is responsible for deleting K8s secrets using information coming through a channel
// of type DeletionRequest.
type Deleter struct {
	client  *kubernetes.Clientset
	reqChan <-chan DeletionRequest
	logger  *slog.Logger
	metrics DeleterMetrics

	gvk schema.GroupVersionKind
}

// DeleterMetrics defines the various metrics that will be generated from this package.
type DeleterMetrics interface {
	ResourceNotFound(ns, gvk string)
	ResourceDeleted(ns, gvk string)
	ResourceDeleteError(ns, gvk string)
}

// DeletionRequest contains the information of the K8s secret to be deleted if not already GC-ed.
type DeletionRequest struct {
	Name      string // name of the secret to be deleted, must be unique within the namespace
	Namespace string // name of the namespace where the secret should be deleted
}

// NewDeleter creates a Deleter instance and returns it along with a channel for sending
// the information of the secret to be deleted.
func NewDeleter(
	client *kubernetes.Clientset,
	logger *slog.Logger,
	metrics DeleterMetrics,
) (*Deleter, chan<- DeletionRequest, error) {
	if logger == nil {
		return nil, nil, utils.ErrNoLogger
	}

	if metrics == nil {
		return nil, nil, utils.ErrNoMetrics
	}

	// create an unbuffered channel so that the separate goroutines are coordinated
	reqChan := make(chan DeletionRequest)

	return &Deleter{
		client:  client,
		reqChan: reqChan,
		logger:  logger,
		metrics: metrics,
		gvk: schema.GroupVersionKind{
			Version: "v1",
			Kind:    "Secret",
			Group:   "",
		},
	}, reqChan, nil
}

func (sc *Deleter) Start(ctx context.Context) error {
	sc.logger.Info("starting secret deletor")

	for req := range sc.reqChan {
		sc.logger.DebugContext(ctx, "request from channel", "request", req)

		// ask K8s API server to delete the requested secret
		err := sc.client.CoreV1().Secrets(req.Namespace).Delete(ctx, req.Name, metav1.DeleteOptions{})
		if kErrors.IsNotFound(err) {
			sc.metrics.ResourceNotFound(req.Namespace, sc.gvk.String())
			sc.logger.InfoContext(ctx, "secret not found in NS", "secret", req.Name, "namespace", req.Namespace)
		} else if statusError, isStatus := err.(*kErrors.StatusError); isStatus { //nolint:errorlint // StatusError does not implement error interface
			sc.metrics.ResourceDeleteError(req.Namespace, sc.gvk.String())
			sc.logger.ErrorContext(ctx,
				"deleting secret in NS",
				"secret", req.Name,
				"namespace", req.Namespace,
				"error", statusError.ErrStatus.Message,
			)
		} else {
			sc.metrics.ResourceDeleted(req.Namespace, sc.gvk.String())
			sc.logger.InfoContext(ctx, "deleted secret in NS", "secret", req.Name, "namespace", req.Namespace)
		}
	}

	return nil
}
