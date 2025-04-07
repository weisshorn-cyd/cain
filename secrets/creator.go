package secrets

import (
	"context"
	"log/slog"
	"maps"

	corev1 "k8s.io/api/core/v1"
	kErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/weisshorn-cyd/cain/utils"
)

// Creator is responsible for creating new K8s secrets using information coming through a channel
// of type CreationRequest.
type Creator struct {
	client  *kubernetes.Clientset
	reqChan <-chan CreationRequest
	logger  *slog.Logger
	metrics CreatorMetrics

	gvk schema.GroupVersionKind
}

// CreatorMetrics defines the various metrics that will be generated from this package.
type CreatorMetrics interface {
	ResourceAlreadyExists(ns, gvk string)
	ResourceCreateError(ns, gvk string)
	ResourceCreated(ns, gvk string)
}

// CreationRequest contains the information of the new K8s secret to be created.
type CreationRequest struct {
	Name      string                 // name of the secret to be created, must be unique within the namespace
	Namespace string                 // name of the namespace where the secret should be created
	KVs       map[string][]byte      // key-value pairs of data to set in the secret
	CtlrRef   *metav1.OwnerReference // the owner of the certificate to be created
}

// NewCreator creates a SecretCreator instance and returns it along with a channel for sending
// the information of the secret to be created.
func NewCreator(
	client *kubernetes.Clientset,
	logger *slog.Logger,
	metrics CreatorMetrics,
) (*Creator, chan<- CreationRequest, error) {
	if logger == nil {
		return nil, nil, utils.ErrNoLogger
	}

	if metrics == nil {
		return nil, nil, utils.ErrNoMetrics
	}

	// create an unbuffered channel so that the separate goroutines are coordinated
	reqChan := make(chan CreationRequest)

	return &Creator{
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

func (sc *Creator) Start(ctx context.Context) error {
	sc.logger.Info("starting secret creator")

	for req := range sc.reqChan {
		sc.logger.DebugContext(ctx, "request from channel", "request", req)

		// create the K8s secret object
		newSecret := &corev1.Secret{}
		newSecret.ObjectMeta = metav1.ObjectMeta{}
		newSecret.SetNamespace(req.Namespace)
		newSecret.SetName(req.Name)
		newSecret.Data = map[string][]byte{}
		newSecret.StringData = map[string]string{}

		maps.Copy(newSecret.Data, req.KVs)

		if req.CtlrRef != nil && req.CtlrRef.UID != "" {
			newSecret.SetOwnerReferences([]metav1.OwnerReference{*req.CtlrRef})
		}

		sc.logger.DebugContext(ctx, "new secret", "secret", newSecret)

		// ask K8s API server to create the requested secret
		createdSecret, err := sc.client.CoreV1().Secrets(req.Namespace).Create(ctx, newSecret, metav1.CreateOptions{})
		if kErrors.IsAlreadyExists(err) {
			sc.metrics.ResourceAlreadyExists(req.Namespace, sc.gvk.String())
			sc.logger.InfoContext(ctx, "secret already exists in NS", "secret", req.Name, "namespace", req.Namespace)
		} else if statusError, isStatus := err.(*kErrors.StatusError); isStatus { //nolint:errorlint // StatusError does not implement error interface
			sc.metrics.ResourceCreateError(req.Namespace, sc.gvk.String())
			sc.logger.ErrorContext(ctx,
				"creating secret in NS",
				"secret", req.Name,
				"namespace", req.Namespace,
				"error", statusError.ErrStatus.Message,
			)
		} else if err != nil {
			sc.metrics.ResourceCreateError(req.Namespace, sc.gvk.String())
			sc.logger.ErrorContext(ctx, "creating secret", "secret", req.Name, "error", err)
		} else {
			sc.metrics.ResourceCreated(req.Namespace, sc.gvk.String())
			sc.logger.InfoContext(ctx, "created secret in NS", "secret", req.Name, "namespace", req.Namespace)
			sc.logger.DebugContext(ctx, "secret from API", "secret", createdSecret)
		}
	}

	return nil
}
