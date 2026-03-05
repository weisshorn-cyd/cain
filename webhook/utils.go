package webhook

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var ErrNoRootOwnerFound = errors.New("no root owner found")

type ContainerResources struct {
	cpu    resources
	memory resources
}

type resources struct {
	limit   resource.Quantity
	request resource.Quantity
}

type ContainerResourcesEnv struct {
	CPULimit   string `default:"500m" desc:"The CPU limit for the cain initcontainer"                             envconfig:"CPU_LIMIT"`
	MemLimit   string `default:"50Mi" desc:"The memory limit for the cain initcontainer"                          envconfig:"MEM_LIMIT"`
	CPURequest string `default:""     desc:"The CPU request for the cain initcontainer, defaults to CPU_LIMIT"    envconfig:"CPU_REQUEST"`
	MemRequest string `default:""     desc:"The memory request for the cain initcontainer, defaults to MEM_LIMIT" envconfig:"MEM_REQUEST"`
}

func NewContainerResources(env ContainerResourcesEnv) (*ContainerResources, error) {
	// resource requests default to limit values if not provided
	if env.CPURequest == "" {
		env.CPURequest = env.CPULimit
	}

	if env.MemRequest == "" {
		env.MemRequest = env.MemLimit
	}

	cpuLimit, err := resource.ParseQuantity(env.CPULimit)
	if err != nil {
		return nil, fmt.Errorf("parsing CPU limit: %w", err)
	}

	memLimit, err := resource.ParseQuantity(env.MemLimit)
	if err != nil {
		return nil, fmt.Errorf("parsing Memory limit: %w", err)
	}

	cpuRequest, err := resource.ParseQuantity(env.CPURequest)
	if err != nil {
		return nil, fmt.Errorf("parsing CPU request: %w", err)
	}

	memRequest, err := resource.ParseQuantity(env.MemRequest)
	if err != nil {
		return nil, fmt.Errorf("parsing Memory request: %w", err)
	}

	return &ContainerResources{
		cpu: resources{
			limit:   cpuLimit,
			request: cpuRequest,
		},
		memory: resources{
			limit:   memLimit,
			request: memRequest,
		},
	}, nil
}

func (res *ContainerResources) ToK8S() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    res.cpu.limit,
			corev1.ResourceMemory: res.memory.limit,
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    res.cpu.request,
			corev1.ResourceMemory: res.memory.request,
		},
	}
}

// rootOwner tries to determine the rootOwner by recursively following owner references.
// If it encounters builtin K8s resources it will get those objects and inspect their owner references,
// if they do not have an owner they are considered the root and the owner reference that refers to them
// from their child is returned.
// The ownerRef parameter is not necessary for the n=0 call, it is used as aggregator when recursing so can be nil.
// If the provided metav1.Object does not have any owner references, this returns a dummy owner reference with only
// the name set to the name of the object.
func rootOwner(
	ctx context.Context,
	client kubernetes.Interface,
	obj metav1.Object,
	ownerRef *metav1.OwnerReference,
	namespace string,
) (*metav1.OwnerReference, error) {
	// if there are no OwnerReferences, we have reached the root object and have found the root object
	ownerRefs := obj.GetOwnerReferences()
	if len(ownerRefs) == 0 {
		if ownerRef == nil {
			return &metav1.OwnerReference{
				Name: obj.GetName(),
			}, nil
		}

		return ownerRef, nil
	}

	kindFuncs := map[string]func(
		ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
	) (metav1.Object, error){
		"Deployment": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.AppsV1().Deployments(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
		"StatefulSet": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.AppsV1().StatefulSets(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
		"ReplicaSet": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.AppsV1().ReplicaSets(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
		"DaemonSet": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.AppsV1().DaemonSets(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
		"CronJob": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.BatchV1().CronJobs(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
		"Job": func(
			ctx context.Context, kClient kubernetes.Interface, ownRef metav1.OwnerReference, ns string,
		) (metav1.Object, error) {
			return kClient.BatchV1().Jobs(ns).Get(ctx, ownRef.Name, metav1.GetOptions{})
		},
	}

	var (
		err          error
		rootOwnerRef *metav1.OwnerReference
	)
	// iterate over the OwnerReferences looking for K8s object Kinds that could be
	// our root object and then recurse up the hierarchy
	for _, ownRef := range ownerRefs {
		kindFunc, ok := kindFuncs[ownRef.Kind]
		if !ok {
			return &ownRef, nil
		}

		obj, err = kindFunc(ctx, client, ownRef, namespace)
		if err != nil {
			return nil, fmt.Errorf("getting %s %q: %w", ownRef.Kind, ownRef.Name, err)
		}

		ownerRef, err := rootOwner(ctx, client, obj, &ownRef, namespace)
		if err != nil {
			return nil, fmt.Errorf("root owner for %s %q: %w", ownRef.Kind, ownRef.Name, err)
		}

		if ownerRef != nil {
			rootOwnerRef = ownerRef
		}
	}

	if rootOwnerRef == nil {
		return nil, ErrNoRootOwnerFound
	}

	return rootOwnerRef, nil
}
