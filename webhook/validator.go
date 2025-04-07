package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	kwhmodel "github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weisshorn-cyd/cain/certificates"
	"github.com/weisshorn-cyd/cain/metadata"
	"github.com/weisshorn-cyd/cain/secrets"
	"github.com/weisshorn-cyd/cain/utils"
)

var errUnsupportedOperation = errors.New("unsupported operation")

type Validator struct {
	extractor        metadata.Extractor
	client           kubernetes.Interface
	caSecret         *utils.CASecret
	caSecretData     map[string][]byte
	secCreationChan  chan<- secrets.CreationRequest
	secDeletionChan  chan<- secrets.DeletionRequest
	certCreationChan chan<- certificates.Info
	logger           *slog.Logger
}

// NewValidator creates a Validator.
func NewValidator(
	extractor metadata.Extractor,
	client kubernetes.Interface,
	caSecret *utils.CASecret,
	caSecretData map[string][]byte,
	secCreationChan chan<- secrets.CreationRequest,
	secDeletionChan chan<- secrets.DeletionRequest,
	certCreationChan chan<- certificates.Info,
	logger *slog.Logger,
) *Validator {
	return &Validator{
		extractor:        extractor,
		client:           client,
		caSecret:         caSecret,
		caSecretData:     caSecretData,
		secCreationChan:  secCreationChan,
		secDeletionChan:  secDeletionChan,
		certCreationChan: certCreationChan,
		logger:           logger,
	}
}

func (validator *Validator) Validate(
	ctx context.Context,
	admRev *kwhmodel.AdmissionReview,
	obj metav1.Object,
) (*kwhvalidating.ValidatorResult, error) {
	// we check if the context is done and close the channels, indicating to the downhill goroutines
	// to stop (secret copier and cert creator)
	select {
	case <-ctx.Done():
		// close the outgoing channels to signal downstream goroutines to stop
		close(validator.secCreationChan)
		close(validator.certCreationChan)
		close(validator.secDeletionChan)

		return nil, fmt.Errorf("context has been cancelled, %w", ctx.Err())
	default: // used to not block if the context is not done yet
	}

	if !validator.extractor.IsInjectionEnabled(obj) {
		validator.logger.InfoContext(ctx, "injection is not enabled on K8s Object")

		return &kwhvalidating.ValidatorResult{
			Valid: true,
		}, nil
	}

	// type assert that the object is in fact a Pod, use the multi-valued return to prevent panics
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		validator.logger.WarnContext(ctx, "no Pod object in provided K8s Object")

		return &kwhvalidating.ValidatorResult{Message: "Provided resource was not a Pod"}, nil
	}

	switch admRev.Operation { //nolint:exhaustive // use a default case since we only support 2 operations
	case kwhmodel.OperationDelete:
		return validator.deleteOperation(pod, admRev), nil
	case kwhmodel.OperationCreate:
		return validator.createResources(ctx, pod, admRev), nil
	default:
		validator.logger.ErrorContext(ctx, "no logic to handle operation", "operation", string(admRev.Operation))

		return nil, fmt.Errorf("admission operation (%s): %w", admRev.Operation, errUnsupportedOperation)
	}
}

func (validator *Validator) deleteOperation(pod *corev1.Pod, admRev *kwhmodel.AdmissionReview) *kwhvalidating.ValidatorResult {
	switch {
	case len(pod.GetOwnerReferences()) == 0:
		if !admRev.DryRun {
			validator.secDeletionChan <- secrets.DeletionRequest{
				Name:      validator.SecretName(pod.GetName()),
				Namespace: admRev.Namespace,
			}
		}

		return &kwhvalidating.ValidatorResult{
			Valid: true,
		}
	default:
		return &kwhvalidating.ValidatorResult{
			Valid: true,
		}
	}
}

func (validator *Validator) createResources(
	ctx context.Context,
	pod *corev1.Pod,
	admRev *kwhmodel.AdmissionReview,
) *kwhvalidating.ValidatorResult {
	rootObj, ownerRef, err := getRootObject(
		ctx, validator.client, pod, nil, admRev.Namespace,
	)
	if err != nil {
		validator.logger.ErrorContext(ctx, "getting root object", "error", err)

		return &kwhvalidating.ValidatorResult{Message: fmt.Sprintf("No root object found for Pod: %v", err)}
	}

	if !admRev.DryRun {
		validator.secCreationChan <- secrets.CreationRequest{
			Name:      validator.SecretName(rootObj.GetName()),
			KVs:       validator.caSecretData,
			Namespace: admRev.Namespace,
			CtlrRef:   ownerRef,
		}
	}

	if validator.extractor.IsJVMEnabled(pod) && !admRev.DryRun {
		certInfo := certificates.Info{
			PodName:            rootObj.GetName(),
			Namespace:          rootObj.GetNamespace(),
			DNSNames:           []string{validator.extractor.JVMCommonName(rootObj)},
			CtlrRef:            ownerRef,
			TruststorePassword: validator.extractor.TruststorePassword(pod),
		}

		validator.certCreationChan <- certInfo
	}

	return &kwhvalidating.ValidatorResult{
		Valid: true,
	}
}

func (validator *Validator) SecretName(ownerName string) string {
	return fmt.Sprintf("%s-%s", validator.caSecret.Name(), ownerName)
}
