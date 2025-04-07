package metrics

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "cainjector"

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
		}, []string{"namespace", "groupVersionKind"}),
		resourceCreateError: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_create_errors_total",
			Help:      "Number of errors when creating a resource in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{"namespace", "groupVersionKind"}),
		resourceCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_created_total",
			Help:      "Number of resources created in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{"namespace", "groupVersionKind"}),
		resourceDeleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_deleted_total",
			Help:      "Number of resources deleted in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{"namespace", "groupVersionKind"}),
		resourceNotFound: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_not_found_total",
			Help:      "Number of resources not found in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{"namespace", "groupVersionKind"}),
		resourceDeleteError: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:      "resource_delete_errors_total",
			Help:      "Number of errors deleting a resource in a namespace",
			Namespace: namespace,
			Subsystem: subsys,
		}, []string{"namespace", "groupVersionKind"}),
	}

	promReg := prometheus.NewPedanticRegistry()

	if err := errors.Join(
		promReg.Register(prom.resourceAlreadyExists),
		promReg.Register(prom.resourceCreateError),
		promReg.Register(prom.resourceCreated),
		promReg.Register(prom.resourceNotFound),
		promReg.Register(prom.resourceDeleted),
		promReg.Register(prom.resourceDeleteError),
	); err != nil {
		return nil, nil, fmt.Errorf("registering metrics collectors: %w", err)
	}

	return prom, promReg, nil
}

func (p *Prometheus) ResourceAlreadyExists(ns, gvk string) {
	p.resourceAlreadyExists.WithLabelValues(ns, gvk).Inc()
}

func (p *Prometheus) ResourceCreateError(ns, gvk string) {
	p.resourceCreateError.WithLabelValues(ns, gvk).Inc()
}

func (p *Prometheus) ResourceCreated(ns, gvk string) {
	p.resourceCreated.WithLabelValues(ns, gvk).Inc()
}

func (p *Prometheus) ResourceDeleted(ns, gvk string) {
	p.resourceDeleted.WithLabelValues(ns, gvk).Inc()
}

func (p *Prometheus) ResourceDeleteError(ns, gvk string) {
	p.resourceDeleteError.WithLabelValues(ns, gvk).Inc()
}

func (p *Prometheus) ResourceNotFound(ns, gvk string) {
	p.resourceNotFound.WithLabelValues(ns, gvk).Inc()
}
