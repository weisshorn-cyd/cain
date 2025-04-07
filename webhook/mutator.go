package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	kwhmodel "github.com/slok/kubewebhook/v2/pkg/model"
	kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weisshorn-cyd/cain/certificates"
	"github.com/weisshorn-cyd/cain/metadata"
	"github.com/weisshorn-cyd/cain/utils"
)

// various names used throughout the mutating webhook.
const (
	caInitContainerName    = "ca-cert-gen"
	caTruststoreVolumeName = "cain-truststore"
)

// locations used by debian like OSes for TLS certs.
const (
	// location for adding new certs.
	debianCASecretVolumeMountPath = "/usr/local/share/ca-certificates/injected" //nolint:gosec // Not a hardcoded credential G101
	// location of certs after `update-ca-certificates`, folder mounted into the pod containers.
	debianCompleteCAVolumeMountPath = "/etc/ssl/certs/"
	debianCompleteCAName            = "ca-certificates.crt"
)

// locations used by redhat/fedora like OSes for TLS certs.
const (
	// location for adding new certs.
	redhatCASecretVolumeMountPath = "/usr/share/pki/ca-trust-source/anchors" //nolint:gosec // Not a hardcoded credential G101
	// location of certs after `update-ca-trust`, folder mounted into the pod containers.
	redhatCompleteCAVolumeMountPath = "/etc/pki/ca-trust/extracted"
	redhatCompleteCAName            = "ca-bundle.trust.crt"
)

// env vars for Python containers.
const (
	requestsCABundleEnvVar = "REQUESTS_CA_BUNDLE"
	sslCertFileEnvVar      = "SSL_CERT_FILE"
)

const (
	fileDefaultMode = int32(420)
)

var errUnrecognisedFamily = errors.New("unrecognised family")

// Mutator is responsible for mutating Pods with a label `ca-injection: <recognised values>`
// and injecting a new init container that creates a new root CA certificate bundle for use by the
// containers for TLS communication.
// The recognised label values are:
//   - debian
//   - redhat
//   - jvm
//
// For the label values `debian` and `redhat`, the mutator will add an init container that
// executes the necessary steps for creating a new root CA bundle with new CA certificates mounted in the
// init container at the correct locations respective of the OS, the result is written into a K8s empty dir
// volume for use by the other containers in the pod which is then mounted at the OS corresponding location
// of the container base image.
//
// The label value `jvm` behaves differently to the OS label values since the JVM does not use
// the OS root CA bundle but rather a truststore by default an OS wide one, but not all OS CA upate scripts
// generate a truststore (redhat does) so we create a cert-manager certificate that contains a truststore,
// the created secret is the mounted into the containers and an environment variable is used to configure
// the JVM to use our provided truststore.
type Mutator struct {
	client             kubernetes.Interface
	extractor          metadata.Extractor
	caSecret           *utils.CASecret
	debianInitImage    string
	redhatInitImage    string
	jvmEnvVariable     string
	containerResources *ContainerResources
	defaultMode        int32
	logger             *slog.Logger
}

// NewMutator creates a Mutator.
func NewMutator(
	extractor metadata.Extractor,
	client kubernetes.Interface,
	caSecret *utils.CASecret,
	debianInitImage, redhatInitImage, jvmEnvVariable string,
	containerResources *ContainerResources,
	logger *slog.Logger,
) *Mutator {
	return &Mutator{
		client:          client,
		extractor:       extractor,
		caSecret:        caSecret,
		debianInitImage: debianInitImage, redhatInitImage: redhatInitImage,
		jvmEnvVariable:     jvmEnvVariable,
		containerResources: containerResources,
		defaultMode:        fileDefaultMode,
		logger:             logger,
	}
}

// Mutate is the method called by the slok/kubewebhook MutatingWebhook implementation, it fulfills
// the mutating.Mutator interface.
func (mut *Mutator) Mutate(
	ctx context.Context,
	admRev *kwhmodel.AdmissionReview,
	obj metav1.Object,
) (*kwhmutating.MutatorResult, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context has been cancelled, %w", ctx.Err())
	default: // used to not block if the context is not done yet
	}

	if !mut.extractor.IsInjectionEnabled(obj) {
		mut.logger.InfoContext(ctx, "injection is not enabled on K8s Object")

		return &kwhmutating.MutatorResult{}, nil
	}

	// retrieve the namespace of the object being mutated
	podNS := admRev.Namespace

	// do not mutate any pods in the 'kube-system' namespace, we could also exclude the `kube-system`
	// NS in the k8s MutatingWebhookConfiguration but prefer to have an extra check mutating an object
	// in the `kube-system` can cause some unforeseen and difficult errors to debug
	if podNS == "kube-system" {
		// returning a zero-values MutatorResult that no changes were done
		// returning an `error` would stop the webhook, so we avoid returning errors unless it is a
		// critical server error
		return &kwhmutating.MutatorResult{}, nil
	}

	// type assert that the object is in fact a Pod, use the multi-valued return to prevent panics
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		mut.logger.WarnContext(ctx, "no Pod object in provided K8s Object")

		return &kwhmutating.MutatorResult{Warnings: []string{"Provided resource was not a Pod"}}, nil
	}

	return mut.injectCA(ctx, pod, podNS)
}

// injectCA is the method for mutating a pod and injecting the CAs.
func (mut *Mutator) injectCA(
	ctx context.Context,
	pod *corev1.Pod,
	namespace string,
) (*kwhmutating.MutatorResult, error) {
	// check for idempotency, does CA init container exist
	for _, initContainer := range pod.Spec.InitContainers {
		if initContainer.Name == caInitContainerName {
			mut.logger.Warn("Pod already has the CA Init Container, not mutating")

			return &kwhmutating.MutatorResult{
				Warnings: []string{"Pod already mutated for CA injection"},
			}, nil
		}
	}

	if err := mut.addCASecretVolumes(
		ctx,
		pod,
		namespace,
		mut.extractor.SecretVolumeName(pod),
		mut.extractor.CaVolumeName(pod),
	); err != nil {
		mut.logger.ErrorContext(ctx, "adding CA secret volumes failed", "error", err)

		return &kwhmutating.MutatorResult{Warnings: []string{"adding CA secret volumes failed"}}, nil
	}

	if mut.shouldAddJVMCA(pod) {
		if err := mut.addJVMSecretAndEnv(ctx, pod, namespace); err != nil {
			mut.logger.ErrorContext(ctx, "adding JVM secret and ENV failed", "error", err)

			return &kwhmutating.MutatorResult{Warnings: []string{"adding JVM secret and ENV failed"}}, nil
		}
	}

	if mut.extractor.IsPythonEnabled(pod) {
		if err := mut.addPythonEnv(pod); err != nil {
			mut.logger.ErrorContext(ctx, "adding Python ENV failed", "error", err)

			return &kwhmutating.MutatorResult{Warnings: []string{"adding Python ENV failed"}}, nil
		}
	}

	// return the mutated pod object
	return &kwhmutating.MutatorResult{
		MutatedObject: pod,
	}, nil
}

// shouldAddJVMCA checks if JVM injection is enabled and check for idempotency, does CA truststore volume exist.
func (mut *Mutator) shouldAddJVMCA(pod *corev1.Pod) bool {
	if !mut.extractor.IsJVMEnabled(pod) {
		return false
	}

	addJVMCA := true

	for _, volume := range pod.Spec.Volumes {
		if volume.Name == caTruststoreVolumeName {
			mut.logger.Warn("Pod already has the CA Truststore Volume, not mutating")

			addJVMCA = false
		}
	}

	return addJVMCA
}

func (mut *Mutator) getCASecretVolumes(
	pod *corev1.Pod,
	rootObjName string,
	caSecretVolumeName string,
	caCompleteVolumeName string,
) []corev1.Volume {
	defaultSecretName := fmt.Sprintf("%s-%s", mut.caSecret.Name(), rootObjName)

	// use a projected volume to allow specifying multiple secrets in a single volume
	projectedSources := make([]corev1.VolumeProjection, 0, len(mut.caSecret.Keys()))

	for defaultIndex, caSecretKey := range mut.caSecret.Keys() {
		projectedSources = append(projectedSources, corev1.VolumeProjection{
			Secret: &corev1.SecretProjection{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaultSecretName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  caSecretKey,
						Path: fmt.Sprintf("injected_ca-%0d.crt", defaultIndex),
					},
				},
			},
		})
	}

	extraCAsToInject, ok := mut.extractor.GetExtraSecretsToInject(pod)
	if ok {
		// iterate over the specified secrets and add them to the volume projection
		for extraIndex, caToInject := range extraCAsToInject {
			// split the secret definition with the format <secret name>/<key within the secret>
			caToInjectSplit := strings.Split(caToInject, "/")
			secretName, secretKey := caToInjectSplit[0], caToInjectSplit[1]

			volProj := corev1.VolumeProjection{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  secretKey,
							Path: fmt.Sprintf("injected_extra_ca-%0d.crt", extraIndex),
						},
					},
				},
			}

			projectedSources = append(projectedSources, volProj)
		}
	}

	// create the volume that will be mounted in the init container containing the secrets that
	// inlcude the CAs to be injected
	caSecretVolume := corev1.Volume{
		Name: caSecretVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &mut.defaultMode,
				Sources:     projectedSources,
			},
		},
	}

	// create the empty dir volume for transferring the newly generated root CA bundle from the init
	// container to the main containers
	completeCAVolume := corev1.Volume{
		Name: caCompleteVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}

	return []corev1.Volume{caSecretVolume, completeCAVolume}
}

func (mut *Mutator) addCASecretVolumes(
	ctx context.Context,
	pod *corev1.Pod,
	namespace string,
	caSecretVolumeName string,
	caCompleteVolumeName string,
) error {
	rootObj, _, err := getRootObject(
		ctx, mut.client, pod, nil, namespace,
	)
	if err != nil {
		return fmt.Errorf("getting root object: %w", err)
	}

	pod.Spec.Volumes = append(
		pod.Spec.Volumes,
		mut.getCASecretVolumes(pod, rootObj.GetName(), caSecretVolumeName, caCompleteVolumeName)...,
	)

	caSecretVolumeMount := corev1.VolumeMount{
		Name:     caSecretVolumeName,
		ReadOnly: true,
	}

	completeCAVolumeMount := corev1.VolumeMount{
		Name: caCompleteVolumeName,
	}

	// create the container object for the init container
	caInitContainer := corev1.Container{
		Name:      caInitContainerName,
		Resources: mut.containerResources.ToK8S(),
	}

	switch mut.extractor.Family(pod) {
	case metadata.DebianFamily:
		caSecretVolumeMount.MountPath = debianCASecretVolumeMountPath
		completeCAVolumeMount.MountPath = debianCompleteCAVolumeMountPath
		caInitContainer.Image = mut.debianInitImage
	case metadata.RedhatFamily:
		caSecretVolumeMount.MountPath = redhatCASecretVolumeMountPath
		completeCAVolumeMount.MountPath = redhatCompleteCAVolumeMountPath
		caInitContainer.Image = mut.redhatInitImage
	}

	// add the volume mounts to the CA injection init container
	caInitContainer.VolumeMounts = []corev1.VolumeMount{
		caSecretVolumeMount,
		completeCAVolumeMount,
	}

	// add the root CA bundle volume to the other existing init containers
	for i := range pod.Spec.InitContainers {
		pod.Spec.InitContainers[i].VolumeMounts = append(
			pod.Spec.InitContainers[i].VolumeMounts,
			completeCAVolumeMount,
		)
	}

	// add the CA injection init container as the first init container
	// ⚠ the definition order does not guarantee execution order ⚠
	pod.Spec.InitContainers = append([]corev1.Container{caInitContainer}, pod.Spec.InitContainers...)

	// add the root CA bundle volume to the existing containers
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, completeCAVolumeMount)
	}

	return nil
}

func (mut *Mutator) addJVMSecretAndEnv(
	ctx context.Context,
	pod *corev1.Pod,
	namespace string,
) error {
	rootObj, _, err := getRootObject(
		ctx, mut.client, pod, nil, namespace,
	)
	if err != nil {
		return fmt.Errorf("getting root object name: %w", err)
	}

	truststoreMountPath, truststorePath := mut.extractor.JVMPath(pod)

	// create the volume for mounting the certificate secret containing the truststore
	vol := corev1.Volume{
		Name: caTruststoreVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  certificates.SecretName(rootObj.GetName()),
				DefaultMode: &mut.defaultMode,
				Items: []corev1.KeyToPath{
					{
						Key:  truststorePath,
						Path: truststorePath,
					},
				},
			},
		},
	}

	// add the volume to the pod
	pod.Spec.Volumes = append(pod.Spec.Volumes, vol)

	volMount := corev1.VolumeMount{
		Name:      caTruststoreVolumeName,
		MountPath: truststoreMountPath,
		ReadOnly:  true,
	}

	truststoreEnv := fmt.Sprintf(
		"-Djavax.net.ssl.trustStore=%s -Djavax.net.ssl.password=%s",
		filepath.Join(truststoreMountPath, truststorePath), mut.extractor.TruststorePassword(pod),
	)

	for index := range pod.Spec.Containers {
		// add the volume to the existing containers
		pod.Spec.Containers[index].VolumeMounts = append(pod.Spec.Containers[index].VolumeMounts, volMount)

		// add the JVM environment variable used to specify a custom truststore
		envSet := false

		for j := range pod.Spec.Containers[index].Env {
			if pod.Spec.Containers[index].Env[j].Name == mut.jvmEnvVariable {
				// if the JVM env var is already specified on the container then append the
				// value needed for the custom truststore
				pod.Spec.Containers[index].Env[j].Value = fmt.Sprintf(
					"%s %s", pod.Spec.Containers[index].Env[j].Value, truststoreEnv,
				)

				envSet = true

				break
			}
		}

		// if the container does not already have the JVM env var, then add it
		if !envSet {
			pod.Spec.Containers[index].Env = append(pod.Spec.Containers[index].Env, corev1.EnvVar{
				Name:  mut.jvmEnvVariable,
				Value: truststoreEnv,
			})
		}
	}

	return nil
}

func (mut *Mutator) addPythonEnv(pod *corev1.Pod) error {
	var certsPath string

	switch mut.extractor.Family(pod) {
	case metadata.DebianFamily:
		certsPath = filepath.Join(debianCompleteCAVolumeMountPath, debianCompleteCAName)
	case metadata.RedhatFamily:
		certsPath = filepath.Join(redhatCompleteCAVolumeMountPath, redhatCompleteCAName)
	default:
		return errUnrecognisedFamily
	}

	for index := range pod.Spec.Containers {
		// add the Python environment variables used to specify a CA file
		pod.Spec.Containers[index].Env = append(pod.Spec.Containers[index].Env, corev1.EnvVar{
			Name:  requestsCABundleEnvVar,
			Value: certsPath,
		})
		pod.Spec.Containers[index].Env = append(pod.Spec.Containers[index].Env, corev1.EnvVar{
			Name:  sslCertFileEnvVar,
			Value: certsPath,
		})
	}

	return nil
}
