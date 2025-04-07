package metadata

import (
	"fmt"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	enabledLabel                 = "cain.%s/enabled"
	extraSecretsAnnotation       = "cain.%s/extra-ca-secrets" //nolint:gosec // Not a hardcoded credential G101
	familyAnnotation             = "cain.%s/family"
	jvmAnnotation                = "cain.%s/jvm"
	pythonAnnotation             = "cain.%s/python"
	caVolumeNameAnnotation       = "cain.%s/ca-volume-name"
	secretVolumeNameAnnotation   = "cain.%s/secret-volume-name" //nolint:gosec // Not a hardcoded credential G101
	jvmCommonNameAnnotation      = "cain.%s/jvm-common-name"
	truststorePasswordAnnotation = "cain.%s/truststore-password"
	jvmPathAnnotation            = "cain.%s/jvm-path"

	truststoreMountPath = "/jvm-truststore/"
	truststorePath      = "truststore.jks"
)

const maxCNLength = 63

const (
	EnabledValue = "true"
)

type Family string

const (
	DebianFamily Family = "debian"
	RedhatFamily Family = "redhat"
)

const (
	caSecretVolumeName   = "ca"
	caCompleteVolumeName = "ca-certs"
)

type Extractor struct {
	domain                       string
	truststorePassword           string
	enabledLabel                 string
	extraSecretsAnnotation       string
	familyAnnotation             string
	jvmAnnotation                string
	pythonAnnotation             string
	caVolumeNameAnnotation       string
	secretVolumeNameAnnotation   string
	jvmCommonNameAnnotation      string
	truststorePasswordAnnotation string
	jvmPathAnnotation            string
}

func NewExtractor(domain, truststorePassword string) Extractor {
	return Extractor{
		domain:                       domain,
		truststorePassword:           truststorePassword,
		enabledLabel:                 fmt.Sprintf(enabledLabel, domain),
		extraSecretsAnnotation:       fmt.Sprintf(extraSecretsAnnotation, domain),
		familyAnnotation:             fmt.Sprintf(familyAnnotation, domain),
		jvmAnnotation:                fmt.Sprintf(jvmAnnotation, domain),
		pythonAnnotation:             fmt.Sprintf(pythonAnnotation, domain),
		caVolumeNameAnnotation:       fmt.Sprintf(caVolumeNameAnnotation, domain),
		secretVolumeNameAnnotation:   fmt.Sprintf(secretVolumeNameAnnotation, domain),
		jvmCommonNameAnnotation:      fmt.Sprintf(jvmCommonNameAnnotation, domain),
		truststorePasswordAnnotation: fmt.Sprintf(truststorePasswordAnnotation, domain),
		jvmPathAnnotation:            fmt.Sprintf(jvmPathAnnotation, domain),
	}
}

func (e Extractor) EnabledLabel() string                 { return e.enabledLabel }
func (e Extractor) ExtraSecretsAnnotation() string       { return e.extraSecretsAnnotation }
func (e Extractor) FamilyAnnotation() string             { return e.familyAnnotation }
func (e Extractor) JVMAnnotation() string                { return e.jvmAnnotation }
func (e Extractor) PythonAnnotation() string             { return e.pythonAnnotation }
func (e Extractor) CaVolumeNameAnnotation() string       { return e.caVolumeNameAnnotation }
func (e Extractor) SecretVolumeNameAnnotation() string   { return e.secretVolumeNameAnnotation }
func (e Extractor) JVMCommonNameAnnotation() string      { return e.jvmCommonNameAnnotation }
func (e Extractor) TruststorePasswordAnnotation() string { return e.truststorePasswordAnnotation }
func (e Extractor) JVMPathAnnotation() string            { return e.jvmPathAnnotation }

func (e Extractor) IsInjectionEnabled(obj metav1.Object) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}

	// check if the object labels contain the CA injection label key
	// use the multi-valued return to prevent panics
	labelValue, ok := labels[e.EnabledLabel()]
	if !ok {
		return false
	}

	return labelValue == EnabledValue
}

func (e Extractor) Family(obj metav1.Object) Family {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return DebianFamily
	}

	annotationValue, ok := annotations[e.FamilyAnnotation()]
	if !ok {
		return DebianFamily
	}

	switch annotationValue {
	case string(RedhatFamily):
		return RedhatFamily
	default:
		return DebianFamily
	}
}

func (e Extractor) IsJVMEnabled(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	annotationValue, ok := annotations[e.JVMAnnotation()]
	if !ok {
		return false
	}

	return annotationValue == EnabledValue
}

func (e Extractor) IsPythonEnabled(obj metav1.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	annotationValue, ok := annotations[e.PythonAnnotation()]
	if !ok {
		return false
	}

	return annotationValue == EnabledValue
}

func (e Extractor) GetExtraSecretsToInject(obj metav1.Object) ([]string, bool) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil, false
	}

	annotationValue, ok := annotations[e.ExtraSecretsAnnotation()]
	if !ok {
		return nil, false
	}

	return strings.Split(annotationValue, ","), true
}

func (e Extractor) CaVolumeName(obj metav1.Object) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return caCompleteVolumeName
	}

	annotationValue, ok := annotations[e.CaVolumeNameAnnotation()]
	if !ok {
		return caCompleteVolumeName
	}

	return annotationValue
}

func (e Extractor) SecretVolumeName(obj metav1.Object) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return caSecretVolumeName
	}

	annotationValue, ok := annotations[e.SecretVolumeNameAnnotation()]
	if !ok {
		return caSecretVolumeName
	}

	return annotationValue
}

func (e Extractor) JVMCommonName(obj metav1.Object) string {
	defaultCommonName := fmt.Sprintf("%s.%s.%s", obj.GetName(), obj.GetNamespace(), e.domain)

	cnLength := len(defaultCommonName)
	if maxCNLength < cnLength {
		defaultCommonName = defaultCommonName[cnLength-maxCNLength:]
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		return defaultCommonName
	}

	annotationValue, ok := annotations[e.JVMCommonNameAnnotation()]
	if !ok {
		return defaultCommonName
	}

	avLength := len(annotationValue)
	if maxCNLength < avLength {
		annotationValue = annotationValue[avLength-maxCNLength:]
	}

	return annotationValue
}

func (e Extractor) TruststorePassword(obj metav1.Object) string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return e.truststorePassword
	}

	annotationValue, ok := annotations[e.TruststorePasswordAnnotation()]
	if !ok {
		return e.truststorePassword
	}

	return annotationValue
}

func (e Extractor) JVMPath(obj metav1.Object) (string, string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return truststoreMountPath, truststorePath
	}

	annotationValue, ok := annotations[e.JVMPathAnnotation()]
	if !ok {
		return truststoreMountPath, truststorePath
	}

	return filepath.Dir(annotationValue), filepath.Base(annotationValue)
}
