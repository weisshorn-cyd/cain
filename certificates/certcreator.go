package certificates

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmMetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certManager "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	kErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"

	"github.com/weisshorn-cyd/cain/secrets"
	"github.com/weisshorn-cyd/cain/utils"
)

// Creator is responsible for creating cert manager certificates containing a truststore for
// use by JVM apps using information coming through a channel
// of type CertInfo.
type Creator struct {
	client             *certManager.Clientset
	issuerName         string
	infoChan           <-chan Info
	secretCreationChan chan<- secrets.CreationRequest
	logger             *slog.Logger
	metrics            CreatorMetrics

	gvk schema.GroupVersionKind
}

// CreatorMetrics defines the various metrics that will be generated from this package.
type CreatorMetrics interface {
	ResourceAlreadyExists(ns, gvk string)
	ResourceCreateError(ns, gvk string)
	ResourceCreated(ns, gvk string)
}

// Info contains the information needed to create a cert manager certificate.
type Info struct {
	// PodName is the name of the Pod that the certificate is intended to be used with
	PodName string
	// Namespace where the certificate should be created
	Namespace string
	// DNSNames that the TLS certificate should contain, not that important since only using the CA
	DNSNames []string
	// TruststorePassword is the password that should be used to encrypt the truststore
	TruststorePassword string
	// CtrlRef is the owner of the certificate to be created
	CtlrRef *metav1.OwnerReference
}

// NewCreator creates a Creator instance and returns it along with a channel for sending the
// information of the certificate to be created.
func NewCreator(
	issuerName string,
	secretCreationChan chan<- secrets.CreationRequest,
	logger *slog.Logger,
	metrics CreatorMetrics,
) (*Creator, chan<- Info, error) {
	if logger == nil {
		return nil, nil, utils.ErrNoLogger
	}

	if metrics == nil {
		return nil, nil, utils.ErrNoMetrics
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating in cluster config, err = %w", err)
	}

	client, err := certManager.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating K8S clientset, err = %w", err)
	}

	// create an unbuffered channel so that the separate goroutines are coordinated
	infoChan := make(chan Info)

	return &Creator{
		client:             client,
		issuerName:         issuerName,
		infoChan:           infoChan,
		secretCreationChan: secretCreationChan,
		logger:             logger,
		metrics:            metrics,
		gvk: schema.GroupVersionKind{
			Group:   "cert-manager.io",
			Version: "v1",
			Kind:    "Certificate",
		},
	}, infoChan, nil
}

func (cc *Creator) Start(ctx context.Context) error {
	cc.logger.Info("starting cert creator")

	for certInfo := range cc.infoChan {
		cc.logger.DebugContext(ctx, "got cert info", "cert_info", certInfo)

		encodedPassword := make([]byte, base64.StdEncoding.EncodedLen(len(certInfo.TruststorePassword)))
		base64.StdEncoding.Encode(encodedPassword, []byte(certInfo.TruststorePassword))

		cc.logger.DebugContext(ctx, "encoded truststore password", "encoded_password", encodedPassword)

		cc.secretCreationChan <- secrets.CreationRequest{
			Name:      TruststorePasswordSecretName(certInfo.PodName),
			Namespace: certInfo.Namespace,
			KVs: map[string][]byte{
				"password": encodedPassword,
			},
			CtlrRef: certInfo.CtlrRef,
		}

		// create the cert manager Certificate object
		cert := cmv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certInfo.PodName,
				Namespace: certInfo.Namespace,
			},
			Spec: cmv1.CertificateSpec{
				CommonName: certInfo.DNSNames[0],
				DNSNames:   certInfo.DNSNames,
				SecretName: SecretName(certInfo.PodName),
				IssuerRef: cmMetav1.ObjectReference{
					Name: cc.issuerName,
					Kind: "ClusterIssuer",
				},
				Keystores: &cmv1.CertificateKeystores{
					JKS: &cmv1.JKSKeystore{
						Create: true,
						PasswordSecretRef: cmMetav1.SecretKeySelector{
							LocalObjectReference: cmMetav1.LocalObjectReference{
								Name: TruststorePasswordSecretName(certInfo.PodName),
							},
							Key: "password",
						},
					},
				},
			},
		}

		if certInfo.CtlrRef != nil && certInfo.CtlrRef.UID != "" {
			cert.SetOwnerReferences([]metav1.OwnerReference{*certInfo.CtlrRef})
		}

		// ask the K8s API server to create the certificate
		_, err := cc.client.CertmanagerV1().Certificates(certInfo.Namespace).
			Create(ctx, &cert, metav1.CreateOptions{})
		if kErrors.IsAlreadyExists(err) {
			cc.metrics.ResourceAlreadyExists(certInfo.Namespace, cc.gvk.String())
			cc.logger.InfoContext(ctx,
				"certificate already exists in NS",
				"cert", certInfo.PodName, "namespaces", certInfo.Namespace,
			)
		} else if statusError, isStatus := err.(*kErrors.StatusError); isStatus { //nolint:errorlint // StatusError does not implement error interface
			cc.metrics.ResourceAlreadyExists(certInfo.Namespace, cc.gvk.String())
			cc.logger.ErrorContext(ctx,
				"creating certificate in NS",
				"cert", certInfo.PodName, "namespaces", certInfo.Namespace, "error", statusError.ErrStatus.Message,
			)
		} else if err != nil {
			cc.metrics.ResourceCreateError(certInfo.Namespace, cc.gvk.String())
			cc.logger.ErrorContext(ctx, "creating certificate", "cert", certInfo.PodName, "error", err)
		} else {
			cc.metrics.ResourceCreated(certInfo.Namespace, cc.gvk.String())
			cc.logger.InfoContext(ctx,
				"created certificate in NS",
				"cert", certInfo.PodName, "namespaces", certInfo.Namespace,
			)
		}
	}

	return nil
}

// SecretName returns the name of the Certificate secret based on the Pod name.
func SecretName(podName string) string {
	return podName + "-truststore-cert"
}

// TruststorePasswordSecretName returns the name of the truststore password secret based on the Pod name.
func TruststorePasswordSecretName(podName string) string {
	return podName + "-truststore-password"
}
