package metrics

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "cain"
	labelNS   = "namespace"
	gvk       = "groupVersionKind"
)

type Prometheus struct {
	resourceAlreadyExists *prometheus.CounterVec
	resourceCreateError   *prometheus.CounterVec
	resourceCreated       *prometheus.CounterVec
	resourceDeleted       *prometheus.CounterVec
	resourceNotFound      *prometheus.CounterVec
	resourceDeleteError   *prometheus.CounterVec
}

func NewPrometheus(subsys string) (*Prometheus, *prometheus.Registry, error) {
	prom := &Prometheus{
		resourceAlreadyExists: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_already_exists_total",
			Help:      "Number of times a resource in a namespace already exists",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
		resourceCreateError: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_create_errors_total",
			Help:      "Number of errors when creating a resource in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
		resourceCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_created_total",
			Help:      "Number of resources created in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
		resourceDeleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_deleted_total",
			Help:      "Number of resources deleted in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
		resourceNotFound: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_not_found_total",
			Help:      "Number of resources not found in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
		resourceDeleteError: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_delete_errors_total",
			Help:      "Number of errors deleting a resource in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{labelNS, gvk}),
	}

	promReg := prometheus.NewPedanticRegistry()

	err := errors.Join(
		promReg.Register(prom.resourceAlreadyExists),
		promReg.Register(prom.resourceCreateError),
		promReg.Register(prom.resourceCreated),
		promReg.Register(prom.resourceNotFound),
		promReg.Register(prom.resourceDeleted),
		promReg.Register(prom.resourceDeleteError),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("registering metrics collectors: %w", err)
	}

	return prom, promReg, nil
}

func (p *Prometheus) ResourceAlreadyExists(labelNS, gvk string) {
	p.resourceAlreadyExists.WithLabelValues(labelNS, gvk).Inc()
}

func (p *Prometheus) ResourceCreateError(labelNS, gvk string) {
	p.resourceCreateError.WithLabelValues(labelNS, gvk).Inc()
}

func (p *Prometheus) ResourceCreated(labelNS, gvk string) {
	p.resourceCreated.WithLabelValues(labelNS, gvk).Inc()
}

func (p *Prometheus) ResourceDeleted(labelNS, gvk string) {
	p.resourceDeleted.WithLabelValues(labelNS, gvk).Inc()
}

func (p *Prometheus) ResourceDeleteError(labelNS, gvk string) {
	p.resourceDeleteError.WithLabelValues(labelNS, gvk).Inc()
}

func (p *Prometheus) ResourceNotFound(labelNS, gvk string) {
	p.resourceNotFound.WithLabelValues(labelNS, gvk).Inc()
}
