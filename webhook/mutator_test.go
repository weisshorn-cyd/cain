package webhook_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/matryer/is"
	"github.com/slok/kubewebhook/v2/pkg/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"

	"github.com/weisshorn-cyd/cain/metadata"
	"github.com/weisshorn-cyd/cain/utils"
	"github.com/weisshorn-cyd/cain/webhook"
)

const caSecretName = "ca-pki-certs/tls.crt" //nolint:gosec // Not a hardcoded credential G101

var (
	mode           = int32(420)
	controllerBool = true
	caSecret       = &utils.CASecret{}
)

func TestCAInjectionMutator_Mutate(t *testing.T) {
	t.Parallel()

	err := caSecret.UnmarshalText([]byte(caSecretName))
	if err != nil {
		t.Error(err)
	}

	ownerRef := metav1.OwnerReference{
		APIVersion:         "apps/v1",
		Kind:               "Deployment",
		Name:               "test-dep",
		UID:                types.UID("test"),
		Controller:         &controllerBool,
		BlockOwnerDeletion: &controllerBool,
	}

	containerResources, err := webhook.NewContainerResources(webhook.ContainerResourcesEnv{ //nolint:exhaustruct // we are relying on the default values for requests
		CPULimit: "500M",
		MemLimit: "50Mi",
	})
	if err != nil {
		t.Error(err)
	}

	k8sContainerResources := containerResources.ToK8S()

	tests := []struct {
		name   string
		pod    *corev1.Pod
		expPod *corev1.Pod
		expErr bool
	}{
		{
			"Basic Pod without owner ref",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/extra-ca-secrets": "s1/t1.crt,s2/t2.crt",
					},
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/extra-ca-secrets": "s1/t1.crt,s2/t2.crt",
					},
					Name:      "test",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "s1",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "t1.crt",
														Path: "injected_extra_ca-0.crt",
													},
												},
											},
										},
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "s2",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "t2.crt",
														Path: "injected_extra_ca-1.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			false,
		},
		{
			"Basic OS Pod with init container",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "test-init-container",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
						{
							Name:  "test-init-container",
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test-dep",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			false,
		},
		{
			"Basic JVM Pod with cert secret",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/jvm": "true",
						// Optional parameter to overwrite the default ${podName}.${namespace}.weisshorn.cyd.ch
						"cain.weisshorn.cyd/jvm-common-name": "a.b.example.com",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/jvm":             "true",
						"cain.weisshorn.cyd/jvm-common-name": "a.b.example.com",
					},
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							Env: []corev1.EnvVar{
								{
									Name:  "JAVA_OPTS_CUSTOM",
									Value: "-Djavax.net.ssl.trustStore=/jvm-truststore/truststore.jks -Djavax.net.ssl.password=changeit",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
								{
									Name:      "cain-truststore",
									ReadOnly:  true,
									MountPath: "/jvm-truststore/",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test-dep",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "cain-truststore",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "test-dep-truststore-cert",
									Items: []corev1.KeyToPath{
										{
											Key:  "truststore.jks",
											Path: "truststore.jks",
										},
									},
									DefaultMode: &mode,
								},
							},
						},
					},
				},
			},
			false,
		},
		{
			"Basic Pod with annotations",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/extra-ca-secrets": "s1/t1.crt,s2/t2.crt",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/extra-ca-secrets": "s1/t1.crt,s2/t2.crt",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test-dep",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "s1",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "t1.crt",
														Path: "injected_extra_ca-0.crt",
													},
												},
											},
										},
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "s2",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "t2.crt",
														Path: "injected_extra_ca-1.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			false,
		},
		{
			"Pod with custom volume name",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/ca-volume-name":     "my-new-ca-volume-name",
						"cain.weisshorn.cyd/secret-volume-name": "my-new-secret-volume-name",
					},
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/ca-volume-name":     "my-new-ca-volume-name",
						"cain.weisshorn.cyd/secret-volume-name": "my-new-secret-volume-name",
					},
					Name:      "test",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "my-new-ca-volume-name",
									MountPath: "/etc/ssl/certs/",
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "my-new-secret-volume-name",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "my-new-ca-volume-name",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "my-new-secret-volume-name",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "my-new-ca-volume-name",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			false,
		},
		{
			"Basic JVM Pod with cert secret and custom truststore password",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/jvm": "true",
						// Optional parameter to overwrite the default ${podName}.${namespace}.weisshorn.cyd.ch
						"cain.weisshorn.cyd/jvm-common-name":     "a.b.example.com",
						"cain.weisshorn.cyd/truststore-password": "custom-pw",
					},
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"cain.weisshorn.cyd/enabled": "true",
					},
					Annotations: map[string]string{
						"cain.weisshorn.cyd/jvm":                 "true",
						"cain.weisshorn.cyd/jvm-common-name":     "a.b.example.com",
						"cain.weisshorn.cyd/truststore-password": "custom-pw",
					},
					OwnerReferences: []metav1.OwnerReference{
						ownerRef,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:  "ca-cert-gen",
							Image: "ghcr.io/weisshorn-cyd/cain-debian-init",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca",
									ReadOnly:  true,
									MountPath: "/usr/local/share/ca-certificates/injected",
								},
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
							},
							Resources: k8sContainerResources,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "busybox",
							Env: []corev1.EnvVar{
								{
									Name:  "JAVA_OPTS_CUSTOM",
									Value: "-Djavax.net.ssl.trustStore=/jvm-truststore/truststore.jks -Djavax.net.ssl.password=custom-pw",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ca-certs",
									MountPath: "/etc/ssl/certs/",
								},
								{
									Name:      "cain-truststore",
									ReadOnly:  true,
									MountPath: "/jvm-truststore/",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ca",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											Secret: &corev1.SecretProjection{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: "ca-pki-certs-test-dep",
												},
												Items: []corev1.KeyToPath{
													{
														Key:  "tls.crt",
														Path: "injected_ca-0.crt",
													},
												},
											},
										},
									},
									DefaultMode: &mode,
								},
							},
						},
						{
							Name: "ca-certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "cain-truststore",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "test-dep-truststore-cert",
									Items: []corev1.KeyToPath{
										{
											Key:  "truststore.jks",
											Path: "truststore.jks",
										},
									},
									DefaultMode: &mode,
								},
							},
						},
					},
				},
			},
			false,
		},
	}

	k8sClient := testclient.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dep",
			Namespace: "default",
		},
	})

	extractor := metadata.NewExtractor("weisshorn.cyd", "changeit")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			is := is.New(t) //nolint:varnamelen // it's supposed to be is
			ca := webhook.NewMutator(
				extractor,
				k8sClient,
				caSecret,
				"ghcr.io/weisshorn-cyd/cain-debian-init", "ghcr.io/weisshorn-cyd/cain-redhat-init",
				"JAVA_OPTS_CUSTOM",
				containerResources,
				slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true, ReplaceAttr: nil})),
			)
			testPod := tt.pod
			mutRes, err := ca.Mutate(t.Context(), &model.AdmissionReview{Namespace: "default", DryRun: true}, testPod)
			resultPod := mutRes.MutatedObject

			if mutRes.Warnings != nil {
				t.Log(mutRes.Warnings)
			}

			if !tt.expErr {
				is.NoErr(err)
				is.Equal(resultPod, tt.expPod)
			}
		})
	}
}
